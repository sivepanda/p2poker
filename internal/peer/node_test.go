package peer_test

import (
	"context"
	"testing"
	"time"

	cryptolog "github.com/sivepanda/p2poker/internal/crypto/log"
	"github.com/sivepanda/p2poker/internal/dispatch"
	"github.com/sivepanda/p2poker/internal/peer"
)

func startDispatch(t *testing.T, ctx context.Context) string {
	t.Helper()

	srv, err := dispatch.NewServer(dispatch.Config{
		Address:  "127.0.0.1:0",
		LeaseTTL: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	addr, err := srv.ListenAndServe(ctx)
	if err != nil {
		t.Fatalf("listen and serve: %v", err)
	}

	return addr
}

func connectNode(t *testing.T, ctx context.Context, dispatchAddr string) *peer.Node {
	t.Helper()

	pk, _, err := cryptolog.GenerateSigner()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}

	node, err := peer.Connect(ctx, peer.Config{
		DispatchAddr: dispatchAddr,
		PeerAddr:     "127.0.0.1:0",
		PublicKey:    pk,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		err := node.Close()
		if err != nil {
			return
		}
	})
	return node
}

func TestMeshAndMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dispatchAddr := startDispatch(t, ctx)

	nodeA := connectNode(t, ctx, dispatchAddr)
	nodeB := connectNode(t, ctx, dispatchAddr)
	nodeC := connectNode(t, ctx, dispatchAddr)

	sessionID, err := nodeA.CreateSession(ctx, "test-session")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := nodeB.JoinSession(ctx, sessionID); err != nil {
		t.Fatalf("node B join: %v", err)
	}
	if err := nodeC.JoinSession(ctx, sessionID); err != nil {
		t.Fatalf("node C join: %v", err)
	}

	// Form mesh
	if err := nodeA.ConnectToPeers(ctx); err != nil {
		t.Fatalf("node A connect to peers: %v", err)
	}
	if err := nodeB.ConnectToPeers(ctx); err != nil {
		t.Fatalf("node B connect to peers: %v", err)
	}
	if err := nodeC.ConnectToPeers(ctx); err != nil {
		t.Fatalf("node C connect to peers: %v", err)
	}

	// Set up message receivers
	received := make(chan peer.Message, 10)
	handler := func(msg peer.Message) {
		received <- msg
	}

	nodeA.Handle("ping", handler)
	nodeB.Handle("ping", handler)
	nodeC.Handle("ping", handler)

	// Node A sends to Node B directly
	if err := nodeA.Send(nodeB.ID(), "ping", "hello from A"); err != nil {
		t.Fatalf("send A->B: %v", err)
	}

	select {
	case msg := <-received:
		if msg.From != nodeA.ID() {
			t.Errorf("expected from %s, got %s", nodeA.ID(), msg.From)
		}
		body, err := peer.Decode[string](msg)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body != "hello from A" {
			t.Errorf("expected 'hello from A', got %q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for direct message")
	}

	// Node C broadcasts to all
	if err := nodeC.Broadcast("ping", "broadcast from C"); err != nil {
		t.Fatalf("broadcast: %v", err)
	}

	got := 0
	timeout := time.After(3 * time.Second)
	for got < 2 {
		select {
		case msg := <-received:
			if msg.From != nodeC.ID() {
				t.Errorf("expected from %s, got %s", nodeC.ID(), msg.From)
			}
			got++
		case <-timeout:
			t.Fatalf("timed out waiting for broadcast, got %d/2", got)
		}
	}
}
