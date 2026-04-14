package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sivepanda/p2poker/internal/sim"
)

func main() {
	dispatchAddr := flag.String("dispatch", "", "dispatch server address (required)")
	numNodes := flag.Int("n", 4, "number of simulated nodes")
	sessionID := flag.String("session", "sim-session", "session id to create/join")
	numRounds := flag.Int("rounds", 3, "number of rounds to simulate")
	flag.Parse()

	if *dispatchAddr == "" {
		fmt.Println("usage: sim -dispatch ADDR [-n N] [-session ID] [-rounds K]")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := sim.RoundsConfig{
		DispatchAddr: *dispatchAddr,
		NumNodes:     *numNodes,
		SessionID:    *sessionID,
		NumRounds:    *numRounds,
	}

	if err := sim.RunRounds(ctx, cfg); err != nil {
		fmt.Printf("sim failed: %v\n", err)
		os.Exit(1)
	}
}
