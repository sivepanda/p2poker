package peer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/sivepanda/p2poker/internal/protocol"
	"github.com/sivepanda/p2poker/internal/transport"
)

const (
	KindPeerHandshake = "peer_handshake"
	KindPeerMessage   = "peer_message"
)

type Config struct {
	DispatchAddr string
	PeerAddr     string
	NodeID       string
}

type Node struct {
	id       string
	peerAddr string

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
}

func Connect(ctx context.Context, cfg Config) (*Node, error) {
	if cfg.DispatchAddr == "" {
		return nil, errors.New("dispatch address must be set")
	}
	if cfg.PeerAddr == "" {
		return nil, errors.New("peer listen address must be set")
	}

	ln, err := net.Listen("tcp", cfg.PeerAddr)
	if err != nil {
		return nil, fmt.Errorf("peer listen: %w", err)
	}

	dialer := &net.Dialer{}
	rawConn, err := dialer.DialContext(ctx, "tcp", cfg.DispatchAddr)
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("dial dispatch: %w", err)
	}

	conn := transport.NewGobConn(rawConn)
	if err := conn.Send(protocol.Frame{
		Kind:     protocol.KindRegisterReq,
		NodeID:   cfg.NodeID,
		PeerAddr: ln.Addr().String(),
	}); err != nil {
		_ = ln.Close()
		_ = rawConn.Close()
		return nil, fmt.Errorf("register request: %w", err)
	}

	resp, err := conn.Receive()
	if err != nil {
		_ = ln.Close()
		_ = rawConn.Close()
		return nil, fmt.Errorf("register response: %w", err)
	}
	if resp.Kind != protocol.KindRegisterResp {
		_ = ln.Close()
		_ = rawConn.Close()
		return nil, errors.New("unexpected register response kind")
	}
	if !resp.Success {
		_ = ln.Close()
		_ = rawConn.Close()
		return nil, fmt.Errorf("register failed: %s", resp.Error)
	}

	n := &Node{
		id:           resp.NodeID,
		peerAddr:     ln.Addr().String(),
		dispatchConn: conn,
		dispatchRaw:  rawConn,
		listener:     ln,
		peers:        make(map[string]*transport.GobConn),
		pending:      make(map[string]chan protocol.Frame),
		handlers:     make(map[string]Handler),
	}

	go n.dispatchReadLoop()
	go n.acceptPeers()

	return n, nil
}

func (n *Node) ID() string {
	return n.id
}

func (n *Node) ListenAddr() string {
	return n.peerAddr
}

func (n *Node) SessionID() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.session
}

func (n *Node) Close() error {
	_ = n.listener.Close()

	n.peersMu.RLock()
	for _, pc := range n.peers {
		_ = pc.Close()
	}
	n.peersMu.RUnlock()

	return n.dispatchConn.Close()
}
