package main

import (
	"fmt"
	"math/big"
	"os"

	"github.com/sivepanda/p2poker/internal/sim"
)

func main() {
	cfg := sim.Config{
		NumNodes: 4,
		Prime:    big.NewInt(13619),
	}

	network, err := sim.NewNetwork(cfg)
	if err != nil {
		fmt.Printf("failed to build simulation: %v\n", err)
		os.Exit(1)
	}

	if err := network.Run(); err != nil {
		fmt.Printf("simulation failed: %v\n", err)
		os.Exit(1)
	}
}
