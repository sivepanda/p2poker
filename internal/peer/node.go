package peer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/sivepanda/p2poker/internal/ephemeral"
	"github.com/sivepanda/p2poker/internal/protocol"
	"github.com/sivepanda/p2poker/internal/transport"
)

const (
	KindPeerHandshake = "peer_handshake"
	KindPeerMessage   = "peer_message"
)

// ErrAlreadyAttached is returned when AttachDispatch is called on a node that
// is already attached to a dispatch server.
var ErrAlreadyAttached = errors.New("node already attached to dispatch")

// errNotAttached is returned by methods that require an active dispatch
// connection when the node has not been attached yet.
var errNotAttached = errors.New("node not attached to dispatch")

type Config struct {
	DispatchAddr string
	PeerAddr     string
	NodeID       string
}

type Node struct {
	peerAddr   string
	prefNodeID string

	dispatchMu   sync.RWMutex
	id           string
	dispatchConn *transport.GobConn
	dispatchRaw  net.Conn

	listener net.Listener

	mu      sync.RWMutex
	session string

	peersMu sync.RWMutex
	peers   map[string]*transport.GobConn

	pendingMu sync.Mutex
	pending   map[string]chan protocol.Frame

	handlersMu sync.RWMutex
	handlers   map[string]Handler

	requestCounter uint64

	store         *ephemeral.Store
	peerPendingMu sync.Mutex
	peerPending   map[string]chan EphemeralReadResponse

	onGameStart func(sessionID string, order []string)
}

// New constructs a node and starts its peer listener, but does NOT attach to a
// dispatch server. Call AttachDispatch to register and begin dispatch RPCs.
func New(cfg Config) (*Node, error) {
	if cfg.PeerAddr == "" {
		return nil, errors.New("peer listen address must be set")
	}

	ln, err := net.Listen("tcp", cfg.PeerAddr)
	if err != nil {
		return nil, fmt.Errorf("peer listen: %w", err)
	}

	n := &Node{
		peerAddr:    ln.Addr().String(),
		prefNodeID:  cfg.NodeID,
		listener:    ln,
		peers:       make(map[string]*transport.GobConn),
		pending:     make(map[string]chan protocol.Frame),
		handlers:    make(map[string]Handler),
		store:       ephemeral.New(),
		peerPending: make(map[string]chan EphemeralReadResponse),
	}

	go n.acceptPeers()

	return n, nil
}

// Connect is a convenience wrapper that constructs a node and immediately
// attaches it to the dispatch server in cfg. Equivalent to New + AttachDispatch.
func Connect(ctx context.Context, cfg Config) (*Node, error) {
	n, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if err := n.AttachDispatch(ctx, cfg.DispatchAddr); err != nil {
		_ = n.Close()
		return nil, err
	}
	return n, nil
}

// AttachDispatch dials the dispatch server, registers this node, and starts
// the dispatch read loop. Returns ErrAlreadyAttached if the node is already
// attached: call DetachDispatch first to switch servers.
func (n *Node) AttachDispatch(ctx context.Context, dispatchAddr string) error {
	if dispatchAddr == "" {
		return errors.New("dispatch address must be set")
	}

	n.dispatchMu.Lock()
	if n.dispatchConn != nil {
		n.dispatchMu.Unlock()
		return ErrAlreadyAttached
	}
	n.dispatchMu.Unlock()

	dialer := &net.Dialer{}
	rawConn, err := dialer.DialContext(ctx, "tcp", dispatchAddr)
	if err != nil {
		return fmt.Errorf("dial dispatch: %w", err)
	}

	conn := transport.NewGobConn(rawConn)
	if err := conn.Send(protocol.Frame{
		Kind:     protocol.KindRegisterReq,
		NodeID:   n.prefNodeID,
		PeerAddr: n.peerAddr,
	}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("register request: %w", err)
	}

	resp, err := conn.Receive()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("register response: %w", err)
	}
	if resp.Kind != protocol.KindRegisterResp {
		_ = conn.Close()
		return errors.New("unexpected register response kind")
	}
	if !resp.Success {
		_ = conn.Close()
		return fmt.Errorf("register failed: %s", resp.Error)
	}

	n.dispatchMu.Lock()
	// Lost a race with another AttachDispatch call.
	if n.dispatchConn != nil {
		n.dispatchMu.Unlock()
		_ = conn.Close()
		return ErrAlreadyAttached
	}
	n.id = resp.NodeID
	n.dispatchConn = conn
	n.dispatchRaw = rawConn
	n.dispatchMu.Unlock()

	go n.dispatchReadLoop(conn)

	return nil
}

// DetachDispatch closes the dispatch connection, fails any in-flight dispatch
// RPCs, and tears down the peer mesh. The peer listener, ephemeral store, and
// registered message handlers are preserved so the node can be re-attached.
func (n *Node) DetachDispatch() error {
	n.dispatchMu.Lock()
	conn := n.dispatchConn
	n.dispatchConn = nil
	n.dispatchRaw = nil
	n.id = ""
	n.dispatchMu.Unlock()

	if conn == nil {
		return nil
	}

	closeErr := conn.Close()

	n.closeAllPending()

	n.peersMu.Lock()
	for id, pc := range n.peers {
		_ = pc.Close()
		delete(n.peers, id)
	}
	n.peersMu.Unlock()

	n.mu.Lock()
	n.session = ""
	n.mu.Unlock()

	return closeErr
}

// IsAttached reports whether the node currently holds a dispatch connection.
func (n *Node) IsAttached() bool {
	n.dispatchMu.RLock()
	defer n.dispatchMu.RUnlock()
	return n.dispatchConn != nil
}

// ID returns the local node identifier (empty when not attached).
func (n *Node) ID() string {
	n.dispatchMu.RLock()
	defer n.dispatchMu.RUnlock()
	return n.id
}

// ListenAddr exposes the node's peer address.
func (n *Node) ListenAddr() string {
	return n.peerAddr
}

// SessionID returns the node's current session String.
func (n *Node) SessionID() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.session
}

// Store returns the node's local ephemeral store.
func (n *Node) Store() *ephemeral.Store {
	return n.store
}

// SetGameStartHandler registers a callback for when dispatch sends a game start.
func (n *Node) SetGameStartHandler(fn func(sessionID string, order []string)) {
	n.onGameStart = fn
}

// Close kills the node and its network resources.
func (n *Node) Close() error {
	_ = n.listener.Close()

	_ = n.DetachDispatch()

	return nil
}

// dispatchSend sends a frame on the current dispatch connection, returning
// errNotAttached if the node is not currently attached.
func (n *Node) dispatchSend(frame protocol.Frame) error {
	n.dispatchMu.RLock()
	conn := n.dispatchConn
	n.dispatchMu.RUnlock()
	if conn == nil {
		return errNotAttached
	}
	return conn.Send(frame)
}
