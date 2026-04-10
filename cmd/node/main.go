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
	"github.com/sivepanda/p2poker/internal/peer"
)

type ChatMessage struct {
	Body string
	Sent time.Time
}

func main() {
	dispatchAddr := flag.String("dispatch", "127.0.0.1:9000", "dispatch server address")
	peerAddr := flag.String("peer-addr", ":0", "peer listen address")
	nodeID := flag.String("id", "", "node id (optional)")
	create := flag.Bool("create", false, "create a new session")
	sessionID := flag.String("session", "", "session id to join")
	sendTo := flag.String("send-to", "", "target node id")
	broadcast := flag.Bool("broadcast", false, "broadcast to all peers in session")
	body := flag.String("body", "hello", "message body for chat payload")
	listPeers := flag.Bool("list-peers", false, "print peer list after joining session")
	rpcAddr := flag.String("rpc-addr", "127.0.0.1:50051", "gRPC listen address for frontend clients (set empty to disable)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	node, err := peer.Connect(ctx, peer.Config{
		DispatchAddr: *dispatchAddr,
		PeerAddr:     *peerAddr,
		NodeID:       *nodeID,
	})
	if err != nil {
		fmt.Printf("connect failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := node.Close(); err != nil {
			fmt.Printf("close failed: %v\n", err)
		}
	}()

	fmt.Printf("connected as %s (peer listening on %s)\n", node.ID(), node.ListenAddr())

	node.Handle("chat", func(msg peer.Message) {
		decoded, err := peer.Decode[ChatMessage](msg)
		if err != nil {
			fmt.Printf("decode error from %s: %v\n", msg.From, err)
			return
		}

		fmt.Printf("[%s] %s\n", msg.From, decoded.Body)
	})

	if *create {
		created, err := node.CreateSession(ctx, *sessionID)
		if err != nil {
			fmt.Printf("create session failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("created session %s\n", created)
	} else if *sessionID != "" {
		if err := node.JoinSession(ctx, *sessionID); err != nil {
			fmt.Printf("join session failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("joined session %s\n", *sessionID)
	}

	node.StartHeartbeat(ctx, 2*time.Second)

	if *listPeers {
		peers, err := node.ListPeers(ctx)
		if err != nil {
			fmt.Printf("list peers failed: %v\n", err)
		} else {
			for _, p := range peers {
				fmt.Printf("  peer %s at %s\n", p.ID, p.Addr)
			}
		}
	}

	if err := node.ConnectToPeers(ctx); err != nil {
		fmt.Printf("mesh connect failed: %v\n", err)
		os.Exit(1)
	}

	if *sendTo != "" {
		if err := node.Send(*sendTo, "chat", ChatMessage{Body: *body, Sent: time.Now()}); err != nil {
			fmt.Printf("send failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("sent chat to %s\n", *sendTo)
	}

	if *broadcast {
		if err := node.Broadcast("chat", ChatMessage{Body: *body, Sent: time.Now()}); err != nil {
			fmt.Printf("broadcast failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("broadcast chat sent")
	}

	if *rpcAddr != "" {
		go func() {
			if err := clientrpc.Run(ctx, *rpcAddr, node); err != nil {
				fmt.Printf("rpc server failed: %v\n", err)
			}
		}()
		fmt.Printf("frontend gRPC enabled on %s\n", *rpcAddr)
	} else {
		fmt.Println("frontend gRPC disabled (set -rpc-addr to enable)")
		fmt.Printf("gRPC server listening on %s\n", *rpcAddr)
	}

	<-ctx.Done()
	fmt.Println("shutting down node")
}
