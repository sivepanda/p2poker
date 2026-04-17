package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sivepanda/p2poker/internal/clientrpc"
	cryptolog "github.com/sivepanda/p2poker/internal/crypto/log"
	"github.com/sivepanda/p2poker/internal/peer"
	"github.com/sivepanda/p2poker/internal/round"
)

func main() {
	dispatchAddr := flag.String("dispatch", "", "dispatch server address (optional; if empty, attach via gRPC AttachDispatch)")
	peerAddr := flag.String("peer-addr", ":0", "peer listen address")
	nodeID := flag.String("id", "", "node id (optional)")
	rpcAddr := flag.String("rpc-addr", "127.0.0.1:50051", "gRPC listen address for frontend clients (set empty to disable)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	node, err := peer.New(peer.Config{
		PeerAddr: *peerAddr,
		NodeID:   *nodeID,
	})
	if err != nil {
		fmt.Printf("node init failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := node.Close(); err != nil {
			fmt.Printf("close failed: %v\n", err)
		}
	}()

	fmt.Printf("node listening on %s (detached)\n", node.ListenAddr())

	// Initialize ephemeral file handlers for remote reads.
	node.InitEphemeralHandlers()

	// Generate ephemeral keypair for this session.
	pk, sk, err := cryptolog.GenerateSigner()
	if err != nil {
		fmt.Printf("keygen failed: %v\n", err)
		os.Exit(1)
	}

	var rpcSrv *clientrpc.Server
	if *rpcAddr != "" {
		rpcSrv = clientrpc.NewServer(node)
		go func() {
			if err := clientrpc.Serve(ctx, *rpcAddr, rpcSrv); err != nil {
				fmt.Printf("rpc server failed: %v\n", err)
			}
		}()
		fmt.Printf("frontend gRPC enabled on %s\n", *rpcAddr)
	} else {
		fmt.Println("frontend gRPC disabled (set -rpc-addr to enable)")
	}

	// When dispatch sends a game start, spin up the round runner.
	node.SetGameStartHandler(func(sessionID string, order []string) {
		fmt.Printf("game starting in session %s, order: %v\n", sessionID, order)
		runner := round.New(node, node.Store(), sk, pk)
		if rpcSrv != nil {
			rpcSrv.SetRunner(runner)
			defer rpcSrv.SetRunner(nil)
		}
		if err := runner.Run(ctx); err != nil {
			fmt.Printf("round runner exited: %v\n", err)
		}
	})

	if *dispatchAddr != "" {
		if err := node.AttachDispatch(ctx, *dispatchAddr); err != nil {
			fmt.Printf("attach dispatch failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("attached to dispatch %s as %s\n", *dispatchAddr, node.ID())
		node.StartHeartbeat(ctx, 2*time.Second)
	} else {
		fmt.Println("waiting for AttachDispatch via gRPC")
		// Heartbeat ticker is safe to start now; it no-ops while detached.
		node.StartHeartbeat(ctx, 2*time.Second)
	}

	<-ctx.Done()
	fmt.Println("shutting down node")
}
