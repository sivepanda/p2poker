package sim

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	cryptolog "github.com/sivepanda/p2poker/internal/crypto/log"
	"github.com/sivepanda/p2poker/internal/game"
	"github.com/sivepanda/p2poker/internal/peer"
	"github.com/sivepanda/p2poker/internal/round"
)

// RoundsConfig parameters the round-lifecycle simulation.
type RoundsConfig struct {
	DispatchAddr string
	NumNodes     int
	SessionID    string
	NumRounds    int
}

// RunRounds attaches NumNodes peer nodes to an already-running dispatch at
// DispatchAddr, forms a mesh inside SessionID, waits for dispatch to push
// game_start, and runs NumRounds of the propose/verify/commit lifecycle
// with each round's proposer submitting a Check action.
func RunRounds(ctx context.Context, cfg RoundsConfig) error {
	if cfg.DispatchAddr == "" {
		return errors.New("dispatch address required")
	}
	if cfg.NumNodes < 2 {
		return errors.New("need at least 2 nodes")
	}
	if cfg.NumRounds <= 0 {
		return errors.New("need at least 1 round")
	}
	if cfg.SessionID == "" {
		cfg.SessionID = "sim-session"
	}

	nodes := make([]*peer.Node, cfg.NumNodes)
	pks := make([]ed25519.PublicKey, cfg.NumNodes)
	sks := make([]ed25519.PrivateKey, cfg.NumNodes)

	// Attach all nodes to dispatch and init handlers.
	for i := 0; i < cfg.NumNodes; i++ {
		pk, sk, err := cryptolog.GenerateSigner()
		if err != nil {
			return fmt.Errorf("node %d keygen: %w", i, err)
		}

		n, err := peer.New(peer.Config{PeerAddr: "127.0.0.1:0", PublicKey: pk})
		if err != nil {
			return fmt.Errorf("node %d new: %w", i, err)
		}
		if err := n.AttachDispatch(ctx, cfg.DispatchAddr); err != nil {
			return fmt.Errorf("node %d attach: %w", i, err)
		}
		n.StartHeartbeat(ctx, 2*time.Second)

		pks[i], sks[i], nodes[i] = pk, sk, n
		fmt.Printf("[sim] node %d attached as %s (peer addr %s)\n", i, n.ID(), n.ListenAddr())
	}
	defer func() {
		for _, n := range nodes {
			_ = n.Close()
		}
	}()

	// Create session on node 0; others join.
	sessID, err := nodes[0].CreateSession(ctx, cfg.SessionID)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	fmt.Printf("[sim] session %s created by %s\n", sessID, nodes[0].ID())

	for i := 1; i < cfg.NumNodes; i++ {
		if err := nodes[i].JoinSession(ctx, sessID); err != nil {
			return fmt.Errorf("node %d join: %w", i, err)
		}
	}

	// Form mesh after everyone has joined.
	for i, n := range nodes {
		if err := n.ConnectToPeers(ctx); err != nil {
			return fmt.Errorf("node %d connect peers: %w", i, err)
		}
	}

	// Dispatch sorts member IDs before broadcasting game_start; mirror that.
	order := make([]string, cfg.NumNodes)
	for i, n := range nodes {
		order[i] = n.ID()
	}
	sort.Strings(order)
	fmt.Printf("[sim] %d nodes joined, waiting for dispatch game_start...\n", cfg.NumNodes)
	startGameErr := nodes[0].StartGame(ctx)
	if startGameErr != nil {
		return startGameErr
	}
	fmt.Printf("[sim] game_start received, order: %v\n", order)
	// Wait until every node has processed game_start and populated Order.
	for {
		allStarted := true
		for _, n := range nodes {
			if !n.Started {
				allStarted = false
				break
			}
		}
		if allStarted {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Register game_start handlers that build+run a Runner per node.
	runners := make([]*round.Runner, cfg.NumNodes)
	runnerReady := make([]chan struct{}, cfg.NumNodes)
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	var runnerWG sync.WaitGroup
	for i := range nodes {
		n := nodes[i]
		idx := i
		runnerReady[idx] = make(chan struct{})

		runnerWG.Add(1)
		go func() {
			defer runnerWG.Done()
			r := round.New(n, n.Store(), sks[idx], pks[idx])
			runners[idx] = r
			close(runnerReady[idx])
			if err := r.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Printf("[sim] runner %s exited: %v\n", n.ID(), err)
			}
		}()
	}

	// Wait until every runner has been constructed (game_start delivered).
	for i := 0; i < cfg.NumNodes; i++ {
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case <-runnerReady[i]:
		}
	}

	// Scripted actions: valid sequence then an illegal raise (exceeds stack).
	actions := []game.Action{
		{Kind: game.ActionRaise, Amount: 100}, // round 0: raise to 100
		{Kind: game.ActionCall},               // round 1: call
		{Kind: game.ActionRaise, Amount: 500}, // round 2: raise to 500
		{Kind: game.ActionCall},               // round 3: call
		{Kind: game.ActionRaise, Amount: 510}, // round 4: ILLEGAL — exceeds stack
	}

	numRounds := cfg.NumRounds
	if numRounds > len(actions) {
		numRounds = len(actions)
	}

	for r := 0; r < numRounds; r++ {
		proposerID := order[r%len(order)]

		localIdx := -1
		for i, n := range nodes {
			if n.ID() == proposerID {
				localIdx = i
				break
			}
		}
		if localIdx < 0 {
			return fmt.Errorf("round %d proposer %s not local", r, proposerID)
		}

		if err := waitForRound(runCtx, runners[localIdx], uint64(r)); err != nil {
			return err
		}

		action := actions[r]
		if err := runners[localIdx].SubmitAction(action); err != nil {
			return fmt.Errorf("round %d submit: %w", r, err)
		}
		fmt.Printf("[sim] round %d: proposer %s queued %v\n", r, proposerID, action)

		for _, rn := range runners {
			if err := waitForRound(runCtx, rn, uint64(r+1)); err != nil {
				return err
			}
		}
	}

	fmt.Printf("[sim] %d rounds committed on all nodes\n", cfg.NumRounds)

	cancelRun()
	runnerWG.Wait()

	for i, rn := range runners {
		fmt.Printf("[sim] node %s final log length: %d\n", nodes[i].ID(), rn.Log().RoundID())
	}

	return nil
}

// waitForRound blocks until r.Log().RoundID() >= target or ctx is done.
func waitForRound(ctx context.Context, r *round.Runner, target uint64) error {
	for {
		if r.Log().RoundID() >= target {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
