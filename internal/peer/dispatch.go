package peer

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/sivepanda/p2poker/internal/protocol"
)

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
				beatCtx, cancel := context.WithTimeout(context.Background(), interval)
				_ = n.Heartbeat(beatCtx)
				cancel()
			}
		}
	}()
}

func (n *Node) dispatchRequest(ctx context.Context, frame protocol.Frame) (protocol.Frame, error) {
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

	if err := n.dispatchConn.Send(frame); err != nil {
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

func (n *Node) dispatchReadLoop() {
	for {
		frame, err := n.dispatchConn.Receive()
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
	}
}

func (n *Node) closeAllPending() {
	n.pendingMu.Lock()
	defer n.pendingMu.Unlock()

	for id, ch := range n.pending {
		close(ch)
		delete(n.pending, id)
	}
}

func (n *Node) nextRequestID() string {
	v := atomic.AddUint64(&n.requestCounter, 1)
	return fmt.Sprintf("%s-%d", n.id, v)
}
