package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sivepanda/p2poker/internal/dispatch"
)

func main() {
	addr := flag.String("addr", ":9000", "dispatch listen address")
	leaseTTL := flag.Duration("lease-ttl", 10*time.Second, "node lease TTL")
	flag.Parse()

	srv, err := dispatch.NewServer(dispatch.Config{
		Address:  *addr,
		LeaseTTL: *leaseTTL,
	})
	if err != nil {
		fmt.Printf("failed to build dispatch server: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("dispatch listening on %s (lease ttl: %s)\n", *addr, leaseTTL.String())
	if err := srv.Run(ctx); err != nil {
		fmt.Printf("dispatch server failed: %v\n", err)
		os.Exit(1)
	}
}
