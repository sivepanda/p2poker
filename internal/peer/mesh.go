package peer

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/sivepanda/p2poker/internal/protocol"
	"github.com/sivepanda/p2poker/internal/transport"
)

// ConnectToPeers dials every peer announced by dispatch.
func (n *Node) ConnectToPeers(ctx context.Context) error {
	peers, err := n.ListPeers(ctx)
	if err != nil {
		return fmt.Errorf("list peers: %w", err)
	}

	for _, p := range peers {
		if p.ID == n.id {
			continue
		}

		n.peersMu.RLock()
		_, exists := n.peers[p.ID]
		n.peersMu.RUnlock()
		if exists {
			continue
		}

		if err := n.dialPeer(ctx, p.ID, p.Addr); err != nil {
			return fmt.Errorf("dial peer %s at %s: %w", p.ID, p.Addr, err)
		}
	}

	return nil
}

// acceptPeers accepts inbound peer TCP connections.
func (n *Node) acceptPeers() {
	for {
		raw, err := n.listener.Accept()
		if err != nil {
			return
		}

		conn := transport.NewGobConn(raw)
		go n.handleIncomingPeer(conn)
	}
}

// handleIncomingPeer validates and registers an inbound peer.
func (n *Node) handleIncomingPeer(conn *transport.GobConn) {
	frame, err := conn.Receive()
	if err != nil {
		_ = conn.Close()
		return
	}
	if frame.Kind != KindPeerHandshake || frame.NodeID == "" {
		_ = conn.Close()
		return
	}

	peerID := frame.NodeID

	if err := conn.Send(protocol.Frame{
		Kind:   KindPeerHandshake,
		NodeID: n.id,
	}); err != nil {
		_ = conn.Close()
		return
	}

	n.peersMu.Lock()
	n.peers[peerID] = conn
	n.peersMu.Unlock()

	n.peerReadLoop(peerID, conn)
}

// dialPeer opens an outbound peer connection and handshakes.
func (n *Node) dialPeer(ctx context.Context, peerID, addr string) error {
	dialer := &net.Dialer{}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}

	conn := transport.NewGobConn(raw)

	if err := conn.Send(protocol.Frame{
		Kind:   KindPeerHandshake,
		NodeID: n.id,
	}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("handshake send: %w", err)
	}

	resp, err := conn.Receive()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("handshake receive: %w", err)
	}
	if resp.Kind != KindPeerHandshake {
		_ = conn.Close()
		return errors.New("unexpected handshake response")
	}

	n.peersMu.Lock()
	n.peers[peerID] = conn
	n.peersMu.Unlock()

	go n.peerReadLoop(peerID, conn)
	return nil
}

// peerReadLoop receives frames and dispatches message handlers.
func (n *Node) peerReadLoop(peerID string, conn *transport.GobConn) {
	defer func() {
		n.peersMu.Lock()
		delete(n.peers, peerID)
		n.peersMu.Unlock()
		_ = conn.Close()
	}()

	for {
		frame, err := conn.Receive()
		if err != nil {
			return
		}

		if frame.Kind != KindPeerMessage {
			continue
		}

		n.handlersMu.RLock()
		h := n.handlers[frame.MessageType]
		n.handlersMu.RUnlock()

		if h != nil {
			h(Message{
				From:    frame.NodeID,
				Type:    frame.MessageType,
				Payload: frame.Payload,
			})
		}
	}
}
