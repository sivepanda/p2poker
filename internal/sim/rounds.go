package sim

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/sivepanda/p2poker/internal/clientrpc"
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

	// RPCBaseAddr, if non-empty (e.g. "127.0.0.1:50051"), starts a clientrpc
	// gRPC server per simulated node. Node i listens at host:(port+i), so
	// frontends can subscribe to each node's event stream. Leave empty to
	// run sim headless.
	RPCBaseAddr string
}

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
	rpcServers := make([]*clientrpc.Server, cfg.NumNodes)

	// simLog prints and fans out to every node's event bus so any subscribed
	// frontend sees the same harness messages that used to land on stdout.
	simLog := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		fmt.Println(msg)
		for _, n := range nodes {
			if n == nil {
				continue
			}
			n.Emit(peer.Event{Kind: "sim", Component: "sim", Message: msg})
		}
	}

	rpcHost, rpcBasePort, err := parseRPCBase(cfg.RPCBaseAddr)
	if err != nil {
		return err
	}

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

		if cfg.RPCBaseAddr != "" {
			rpcSrv := clientrpc.NewServer(n)
			rpcServers[i] = rpcSrv
			addr := fmt.Sprintf("%s:%d", rpcHost, rpcBasePort+i)
			go func(a string, s *clientrpc.Server) {
				if err := clientrpc.Serve(ctx, a, s); err != nil {
					fmt.Printf("[sim] rpc server %s exited: %v\n", a, err)
				}
			}(addr, rpcSrv)
			simLog("[sim] node %d gRPC listening on %s", i, addr)
		}

		simLog("[sim] node %d attached as %s (peer addr %s)", i, n.ID(), n.ListenAddr())
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
	simLog("[sim] session %s created by %s", sessID, nodes[0].ID())

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
	simLog("[sim] %d nodes joined, waiting for dispatch game_start...", cfg.NumNodes)
	startGameErr := nodes[0].StartGame(ctx)
	if startGameErr != nil {
		return startGameErr
	}
	simLog("[sim] game_start received, order: %v", order)
	// Wait until every node has processed game_start and published runtime state.
	for _, n := range nodes {
		if err := n.WaitForGameStart(ctx); err != nil {
			return err
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
			sc := n.SessionConfig()
			r := round.New(n, n.Store(), sks[idx], pks[idx], round.Config{
				TimeoutInterval: sc.TimeoutInterval,
				MaxAttempts:     sc.MaxAttempts,
			})
			runners[idx] = r
			if rpcServers[idx] != nil {
				rpcServers[idx].SetRunner(r)
				defer rpcServers[idx].SetRunner(nil)
			}
			close(runnerReady[idx])
			if err := r.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
				simLog("[sim] runner %s exited: %v", n.ID(), err)
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

	sessionCfg := nodes[0].SessionConfig()
	timeoutInterval := sessionCfg.TimeoutInterval
	if timeoutInterval <= 0 {
		timeoutInterval = 30 * time.Second
	}
	maxAttempts := sessionCfg.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}

	// Per-round scripts for the proposer. A script can also assert exact
	// per-attempt event sequences to validate attempt-counter behavior.
	scripts := []roundScript{
		{Attempts: []game.Action{{Kind: game.ActionRaise, Amount: 100}}},
		{Attempts: []game.Action{{Kind: game.ActionFold}}},
		{Attempts: []game.Action{{Kind: game.ActionCall}}},
		{Attempts: []game.Action{{Kind: game.ActionCall}}},
		{Attempts: []game.Action{
			{Kind: game.ActionRaise, Amount: 1_000_000}, // illegal
			{Kind: game.ActionRaise, Amount: 1_000_000}, // illegal again (same attempt)
			{Kind: game.ActionRaise, Amount: 1_000_000}, // illegal again (same attempt)
			{Kind: game.ActionCheck},                    // legal retry post-flop (CurrentBet reset)
		}, ExpectedEvents: []expectedEvent{
			{Attempt: 0, Outcome: "rejected"},
			{Attempt: 0, Outcome: "rejected"},
			{Attempt: 0, Outcome: "rejected"},
			{Attempt: 0, Outcome: "committed"},
		}},
		{Attempts: []game.Action{
			{Kind: game.ActionRaise, Amount: 1_000_000}, // one bad action, then stall
		}, ExpectAutoFold: true,
			ExpectedEvents:      expectedAutoFoldEvents(maxAttempts, true),
			ExpectedDurationMin: timeoutInterval * time.Duration(maxAttempts) / 2,
			ExpectedDurationMax: timeoutInterval*time.Duration(maxAttempts) + 2*time.Second,
		},
	}

	numRounds := cfg.NumRounds
	if numRounds > len(scripts) {
		numRounds = len(scripts)
	}

	for r := 0; r < numRounds; r++ {
		// Every runner must have reached round r before we can identify the
		// proposer. ExpectedNextPlayer is fold-aware, so it only stabilizes
		// once prior rounds have committed (or auto-folded) everywhere.
		for _, rn := range runners {
			if err := waitForRound(runCtx, rn, uint64(r)); err != nil {
				return err
			}
		}

		proposerSeat := runners[0].Log().ExpectedNextPlayer()
		proposerID := order[proposerSeat]
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

		report, err := driveScriptedRound(runCtx, runners[localIdx], nodes, r, proposerID, scripts[r].Attempts)
		if err != nil {
			return err
		}
		if scripts[r].ExpectAutoFold && report.Outcome != "auto_fold" {
			return fmt.Errorf("round %d expected auto_fold, got %s", r, report.Outcome)
		}
		if !scripts[r].ExpectAutoFold && report.Outcome != "committed" {
			return fmt.Errorf("round %d expected committed, got %s", r, report.Outcome)
		}
		if len(scripts[r].ExpectedEvents) > 0 {
			if err := verifyExpectedEvents(report.Events, scripts[r].ExpectedEvents); err != nil {
				return fmt.Errorf("round %d event sequence mismatch: %w", r, err)
			}
		}
		if scripts[r].ExpectedDurationMin > 0 && report.Duration < scripts[r].ExpectedDurationMin {
			return fmt.Errorf("round %d completed too quickly: got %s, want >= %s", r, report.Duration, scripts[r].ExpectedDurationMin)
		}
		if scripts[r].ExpectedDurationMax > 0 && report.Duration > scripts[r].ExpectedDurationMax {
			return fmt.Errorf("round %d took too long: got %s, want <= %s", r, report.Duration, scripts[r].ExpectedDurationMax)
		}

		for _, rn := range runners {
			if err := waitForRound(runCtx, rn, uint64(r+1)); err != nil {
				return err
			}
		}

		if scripts[r].ExpectAutoFold {
			// Every replica must agree the proposer is now folded.
			for i, rn := range runners {
				state := game.Replay(rn.Log().Entries(), rn.Log().NumPlayers(), rn.Log().StartingStack())
				if !state.Folded[proposerSeat] {
					return fmt.Errorf("round %d auto-fold: node %s seat %d not folded", r, nodes[i].ID(), proposerSeat)
				}
			}
			simLog("[sim] round %d auto-fold confirmed across %d nodes", r, len(runners))
		}
	}

	simLog("[sim] %d rounds simulated on all nodes", numRounds)

	cancelRun()
	runnerWG.Wait()

	for i, rn := range runners {
		simLog("[sim] node %s final log length: %d", nodes[i].ID(), rn.Log().RoundID())
	}

	return nil
}

// parseRPCBase splits "host:port" into host and base port. Empty addr
// disables RPC and returns zero values.
func parseRPCBase(addr string) (string, int, error) {
	if addr == "" {
		return "", 0, nil
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("rpc base addr %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("rpc base port %q: %w", portStr, err)
	}
	return host, port, nil
}

// roundScript packages a per-round sequence of proposer attempts.
type roundScript struct {
	Attempts       []game.Action
	ExpectAutoFold bool
	ExpectedEvents []expectedEvent

	ExpectedDurationMin time.Duration
	ExpectedDurationMax time.Duration
}

type expectedEvent struct {
	Attempt uint32
	Outcome string
}

type roundReport struct {
	Outcome  string
	Events   []round.AttemptEvent
	Duration time.Duration
}

// driveScriptedRound submits scripted actions to the proposer and returns the
// terminal outcome plus the full event stream observed for that round.
func driveScriptedRound(
	ctx context.Context,
	r *round.Runner,
	nodes []*peer.Node,
	roundID int,
	proposerID string,
	attempts []game.Action,
) (roundReport, error) {
	simLog := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		fmt.Println(msg)
		for _, n := range nodes {
			n.Emit(peer.Event{Kind: "sim", Component: "sim", Message: msg})
		}
	}

	start := time.Now()
	events := make([]round.AttemptEvent, 0, len(attempts)+4)
	submitted := 0
	currentAttempt := uint32(0)
	for {
		if submitted < len(attempts) {
			if err := r.SubmitAction(attempts[submitted]); err != nil {
				if errors.Is(err, round.ErrIllegalAction) {
					ev := round.AttemptEvent{
						RoundID: uint64(roundID),
						Attempt: currentAttempt,
						Outcome: "rejected",
						Reason:  err.Error(),
					}
					events = append(events, ev)
					simLog("[sim] round %d attempt %d rejected: %s", roundID, currentAttempt, ev.Reason)
					submitted++
					continue
				}
				return roundReport{}, fmt.Errorf("round %d attempt %d submit: %w", roundID, submitted, err)
			}
			simLog("[sim] round %d attempt %d: proposer %s queued %v",
				roundID, submitted, proposerID, attempts[submitted])
			submitted++
		}

		ev, err := nextRoundEvent(ctx, r, uint64(roundID))
		if err != nil {
			return roundReport{}, err
		}
		events = append(events, ev)
		if ev.Outcome == "timeout" {
			currentAttempt = ev.Attempt + 1
		} else {
			currentAttempt = ev.Attempt
		}
		switch ev.Outcome {
		case "committed":
			simLog("[sim] round %d committed (attempt %d)", roundID, ev.Attempt)
			return roundReport{Outcome: "committed", Events: events, Duration: time.Since(start)}, nil
		case "auto_fold":
			simLog("[sim] round %d AUTO-FOLD", roundID)
			return roundReport{Outcome: "auto_fold", Events: events, Duration: time.Since(start)}, nil
		case "rejected", "timeout":
			simLog("[sim] round %d attempt %d %s: %s",
				roundID, ev.Attempt, ev.Outcome, ev.Reason)
		}
	}
}

func verifyExpectedEvents(got []round.AttemptEvent, want []expectedEvent) error {
	if len(got) != len(want) {
		return fmt.Errorf("got %d events, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Attempt != want[i].Attempt || got[i].Outcome != want[i].Outcome {
			return fmt.Errorf("event %d: got (%d,%s), want (%d,%s)", i, got[i].Attempt, got[i].Outcome, want[i].Attempt, want[i].Outcome)
		}
	}
	return nil
}

func expectedAutoFoldEvents(maxAttempts uint32, includeInitialReject bool) []expectedEvent {
	out := make([]expectedEvent, 0, int(maxAttempts)+2)
	if includeInitialReject {
		out = append(out, expectedEvent{Attempt: 0, Outcome: "rejected"})
	}
	for a := uint32(0); a < maxAttempts; a++ {
		out = append(out, expectedEvent{Attempt: a, Outcome: "timeout"})
	}
	out = append(out, expectedEvent{Attempt: 0, Outcome: "auto_fold"})
	return out
}

// nextRoundEvent drains events until one matches target. Stale events (from
// prior rounds this runner observed as verifier) are skipped; re-entering
// the submit loop on them would double-submit and hit "action already pending".
func nextRoundEvent(ctx context.Context, r *round.Runner, target uint64) (round.AttemptEvent, error) {
	for {
		select {
		case <-ctx.Done():
			return round.AttemptEvent{}, ctx.Err()
		case ev := <-r.Events():
			if ev.RoundID == target {
				return ev, nil
			}
		}
	}
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
