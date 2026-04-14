package peer

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"

	"github.com/sivepanda/p2poker/internal/protocol"
)

type Message struct {
	From      string
	SessionID string
	Type      string
	Payload   []byte
}

type Handler func(Message)

type PeerInfo struct {
	ID   string
	Addr string
}

// Send transmits a typed payload to the named peer.
func (n *Node) Send(targetID, messageType string, payload any) error {
	buf, err := encodePayload(payload)
	if err != nil {
		return err
	}

	n.peersMu.RLock()
	pc, ok := n.peers[targetID]
	n.peersMu.RUnlock()
	if !ok {
		return fmt.Errorf("no connection to peer %s", targetID)
	}

	return pc.Send(protocol.Frame{
		Kind:        KindPeerMessage,
		NodeID:      n.ID(),
		MessageType: messageType,
		Payload:     buf,
	})
}

// Broadcast sends a typed payload to all peers.
func (n *Node) Broadcast(messageType string, payload any) error {
	buf, err := encodePayload(payload)
	if err != nil {
		return err
	}

	frame := protocol.Frame{
		Kind:        KindPeerMessage,
		NodeID:      n.ID(),
		MessageType: messageType,
		Payload:     buf,
	}

	n.peersMu.RLock()
	defer n.peersMu.RUnlock()

	for id, pc := range n.peers {
		if err := pc.Send(frame); err != nil {
			return fmt.Errorf("send to peer %s: %w", id, err)
		}
	}

	return nil
}

// Handle registers a handler for the given message type.
func (n *Node) Handle(messageType string, handler Handler) {
	n.handlersMu.Lock()
	defer n.handlersMu.Unlock()
	n.handlers[messageType] = handler
}

// ListPeers asks dispatch for the current peer list.
func (n *Node) ListPeers(ctx context.Context) ([]PeerInfo, error) {
	resp, err := n.dispatchRequest(ctx, protocol.Frame{Kind: protocol.KindListPeersReq})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, errors.New(resp.Error)
	}

	peers := make([]PeerInfo, len(resp.PeerIDs))
	for i := range resp.PeerIDs {
		peers[i] = PeerInfo{ID: resp.PeerIDs[i], Addr: resp.PeerAddresses[i]}
	}
	return peers, nil
}

// Decode deserializes the payload of a peer message.
func Decode[T any](msg Message) (T, error) {
	var out T
	if err := gob.NewDecoder(bytes.NewReader(msg.Payload)).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

// encodePayload serializes a payload using gob encoding.
func encodePayload(payload any) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(payload); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
