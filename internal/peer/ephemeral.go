package peer

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	MsgEphemeralReadReq  = "ephemeral_read_req"
	MsgEphemeralReadResp = "ephemeral_read_resp"
)

// EphemeralReadRequest is sent to a remote peer to read from their store.
type EphemeralReadRequest struct {
	RequestID string
	Key       string
}

// EphemeralReadResponse is the reply carrying the value (or not-found).
type EphemeralReadResponse struct {
	RequestID string
	Key       string
	Value     []byte
	Found     bool
}

// InitEphemeralHandlers registers the message handlers for ephemeral read
// request/response on this node. Call once after Connect().
func (n *Node) InitEphemeralHandlers() {
	n.Handle(MsgEphemeralReadReq, func(msg Message) {
		req, err := Decode[EphemeralReadRequest](msg)
		if err != nil {
			return
		}

		value, found := n.store.Get(req.Key)
		_ = n.Send(msg.From, MsgEphemeralReadResp, EphemeralReadResponse{
			RequestID: req.RequestID,
			Key:       req.Key,
			Value:     value,
			Found:     found,
		})
	})

	n.Handle(MsgEphemeralReadResp, func(msg Message) {
		resp, err := Decode[EphemeralReadResponse](msg)
		if err != nil {
			return
		}

		n.peerPendingMu.Lock()
		ch, ok := n.peerPending[resp.RequestID]
		n.peerPendingMu.Unlock()

		if ok {
			select {
			case ch <- resp:
			default:
			}
		}
	})
}

// ReadRemote sends a single read request to targetID for the given key.
// Returns (value, found, error).
func (n *Node) ReadRemote(ctx context.Context, targetID, key string) ([]byte, bool, error) {
	requestID := n.nextRequestID()

	ch := make(chan EphemeralReadResponse, 1)
	n.peerPendingMu.Lock()
	n.peerPending[requestID] = ch
	n.peerPendingMu.Unlock()

	defer func() {
		n.peerPendingMu.Lock()
		delete(n.peerPending, requestID)
		n.peerPendingMu.Unlock()
	}()

	if err := n.Send(targetID, MsgEphemeralReadReq, EphemeralReadRequest{
		RequestID: requestID,
		Key:       key,
	}); err != nil {
		return nil, false, fmt.Errorf("send ephemeral read to %s: %w", targetID, err)
	}

	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case resp := <-ch:
		return resp.Value, resp.Found, nil
	}
}

// PollRemote polls a remote peer for a key until it appears or ctx is cancelled.
// Uses exponential backoff starting at 50ms, capped at 500ms.
func (n *Node) PollRemote(ctx context.Context, targetID, key string) ([]byte, error) {
	backoff := 50 * time.Millisecond
	const maxBackoff = 500 * time.Millisecond

	for {
		value, found, err := n.ReadRemote(ctx, targetID, key)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			// Transient error (peer not connected yet, etc.) — retry.
		} else if found {
			return value, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
