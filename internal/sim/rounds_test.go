package sim_test

import (
	"context"
	"testing"
	"time"

	"github.com/sivepanda/p2poker/internal/dispatch"
	"github.com/sivepanda/p2poker/internal/sim"
)

func startDispatch(t *testing.T, ctx context.Context) string {
	t.Helper()

	srv, err := dispatch.NewServer(dispatch.Config{
		Address:         "127.0.0.1:0",
		LeaseTTL:        30 * time.Second,
		TimeoutInterval: 3 * time.Second,
		MaxAttempts:     3,
	})
	if err != nil {
		t.Fatalf("new dispatch: %v", err)
	}

	addr, err := srv.ListenAndServe(ctx)
	if err != nil {
		t.Fatalf("listen and serve: %v", err)
	}
	return addr
}

func TestRunRounds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dispatchAddr := startDispatch(t, ctx)

	cfg := sim.RoundsConfig{
		DispatchAddr: dispatchAddr,
		NumNodes:     4,
		SessionID:    "rounds-sim",
		NumRounds:    11,
	}

	if err := sim.RunRounds(ctx, cfg); err != nil {
		t.Fatalf("RunRounds: %v", err)
	}
}
