package peer

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/sivepanda/p2poker/internal/protocol"
	"github.com/sivepanda/p2poker/internal/transport"
)

// CreateSession asks dispatch to create (or name) a session.
func (n *Node) CreateSession(ctx context.Context, sessionID string) (string, error) {
	resp, err := n.dispatchRequest(ctx, protocol.Frame{
		Kind:      protocol.KindCreateSessionReq,
		SessionID: sessionID,
	})
	if err != nil {
		return "", err
	}
	if !resp.Success {
		return "", errors.New(resp.Error)
	}

	n.mu.Lock()
	n.session = resp.SessionID
	n.mu.Unlock()

	return resp.SessionID, nil
}

// JoinSession asks dispatch to join an existing session.
func (n *Node) JoinSession(ctx context.Context, sessionID string) error {
	resp, err := n.dispatchRequest(ctx, protocol.Frame{
		Kind:      protocol.KindJoinSessionReq,
		SessionID: sessionID,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errors.New(resp.Error)
	}

	n.mu.Lock()
	n.session = resp.SessionID
	n.mu.Unlock()

	return nil
}

// ListSessions returns session IDs known by dispatch.
func (n *Node) ListSessions(ctx context.Context) ([]string, error) {
	resp, err := n.dispatchRequest(ctx, protocol.Frame{Kind: protocol.KindListSessionsReq})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, errors.New(resp.Error)
	}
	return resp.SessionIDs, nil
}

// Heartbeat renews this node's dispatch lease.
func (n *Node) Heartbeat(ctx context.Context) error {
	resp, err := n.dispatchRequest(ctx, protocol.Frame{Kind: protocol.KindHeartbeatReq})
	if err != nil {
		return err
	}
	if !resp.Success {
		return errors.New(resp.Error)
	}
	return nil
}

// StartHeartbeat launches periodic lease renewals. Ticks while attached;
// silently skips when the node is detached.
func (n *Node) StartHeartbeat(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !n.IsAttached() {
					continue
				}
				beatCtx, cancel := context.WithTimeout(context.Background(), interval)
				_ = n.Heartbeat(beatCtx)
				cancel()
			}
		}
	}()
}

// dispatchRequest sends a frame and waits for its response.
func (n *Node) dispatchRequest(ctx context.Context, frame protocol.Frame) (protocol.Frame, error) {
	if !n.IsAttached() {
		return protocol.Frame{}, errNotAttached
	}

	requestID := n.nextRequestID()
	frame.RequestID = requestID

	respCh := make(chan protocol.Frame, 1)

	n.pendingMu.Lock()
	n.pending[requestID] = respCh
	n.pendingMu.Unlock()

	defer func() {
		n.pendingMu.Lock()
		delete(n.pending, requestID)
		n.pendingMu.Unlock()
	}()

	if err := n.dispatchSend(frame); err != nil {
		return protocol.Frame{}, err
	}

	select {
	case <-ctx.Done():
		return protocol.Frame{}, ctx.Err()
	case resp, ok := <-respCh:
		if !ok {
			return protocol.Frame{}, errors.New("dispatch connection closed")
		}
		return resp, nil
	}
}

// dispatchReadLoop routes dispatch responses to waiting callers. The conn is
// captured at goroutine start so a Detach + Attach can spawn a new loop on a
// new connection without racing on n.dispatchConn.
func (n *Node) dispatchReadLoop(conn *transport.GobConn) {
	for {
		frame, err := conn.Receive()
		if err != nil {
			n.closeAllPending()
			return
		}

		if frame.RequestID != "" {
			n.pendingMu.Lock()
			respCh, ok := n.pending[frame.RequestID]
			n.pendingMu.Unlock()
			if ok {
				respCh <- frame
				continue
			}
		}

		// Handle push frames from dispatch (no RequestID).
		switch frame.Kind {
		case protocol.KindGameStart:
			if n.onGameStart != nil {
				go n.onGameStart(frame.SessionID, frame.PeerIDs)
			}
		}
	}
}

// closeAllPending closes every waiting dispatch response channel.
func (n *Node) closeAllPending() {
	n.pendingMu.Lock()
	defer n.pendingMu.Unlock()

	for id, ch := range n.pending {
		close(ch)
		delete(n.pending, id)
	}
}

// nextRequestID generates a unique request ID for dispatch calls.
func (n *Node) nextRequestID() string {
	v := atomic.AddUint64(&n.requestCounter, 1)
	return fmt.Sprintf("%s-%d", n.ID(), v)
}
