package round

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/gob"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sivepanda/p2poker/internal/ephemeral"
	"github.com/sivepanda/p2poker/internal/game"
	"github.com/sivepanda/p2poker/internal/peer"
)

// Role describes what a node does during a given round.
type Role uint8

const (
	// RoleProposer is the active player who hosts the proposal file.
	RoleProposer Role = iota
	// RoleVerifier polls for the proposal, verifies it, then hosts a verify receipt.
	RoleVerifier
)

// Config holds per-session rules injected by the dispatcher on GameStart.
type Config struct {
	TimeoutInterval time.Duration
	MaxAttempts     uint32
}

// AttemptEvent is emitted after each attempt's outcome is observed.
type AttemptEvent struct {
	RoundID uint64
	Attempt uint32
	Outcome string // "committed" | "rejected" | "timeout" | "auto_fold"
	Reason  string
}

type attemptOutcome int

const (
	attemptCommitted attemptOutcome = iota
	attemptTimeout                  // deadline or peer failure
)

var (
	ErrNotYourTurn          = errors.New("not our turn")
	ErrActionAlreadyPending = errors.New("action already pending")
	ErrIllegalAction        = errors.New("illegal action")
)

// Runner orchestrates the propose/verify/commit lifecycle for one node.
type Runner struct {
	node  *peer.Node
	store *ephemeral.Store
	log   *game.Log

	sk  ed25519.PrivateKey
	pk  ed25519.PublicKey
	cfg Config

	actionCh chan game.Action
	events   chan AttemptEvent
}

// New creates a Runner from the game start data. cfg carries session rules
// (TimeoutInterval, MaxAttempts) that the dispatcher broadcast in GameStart.
func New(
	node *peer.Node,
	store *ephemeral.Store,
	sk ed25519.PrivateKey,
	pk ed25519.PublicKey,
	cfg Config,
) *Runner {
	if cfg.TimeoutInterval <= 0 {
		cfg.TimeoutInterval = 30 * time.Second
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 3
	}
	order := node.OrderSnapshot()
	gameLog := game.NewLog(uint8(len(order)), uint64(node.Money()))

	return &Runner{
		node:     node,
		store:    store,
		log:      gameLog,
		sk:       sk,
		pk:       pk,
		cfg:      cfg,
		actionCh: make(chan game.Action, 1),
		events:   make(chan AttemptEvent, 16),
	}
}

// Log returns the runner's game log.
func (r *Runner) Log() *game.Log {
	return r.log
}

// Events streams per-attempt outcomes for test harnesses and sim drivers.
func (r *Runner) Events() <-chan AttemptEvent {
	return r.events
}

// SubmitAction queues an action for when it is this node's turn.
func (r *Runner) SubmitAction(action game.Action) error {
	if r.currentRole() != RoleProposer {
		return ErrNotYourTurn
	}

	state := game.Replay(r.log.Entries(), r.log.NumPlayers(), r.log.StartingStack())
	if err := state.ValidateAction(uint8(r.node.SeatIdx), action); err != nil {
		return fmt.Errorf("%w: %v", ErrIllegalAction, err)
	}

	select {
	case r.actionCh <- action:
		return nil
	default:
		return ErrActionAlreadyPending
	}
}

// Run starts the round lifecycle loop. Blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.node.WaitForGameStart(ctx); err != nil {
		return err
	}

	if err := r.node.RunShuffle(ctx); err != nil {
		return fmt.Errorf("shuffle: %w", err)
	}
	if err := r.node.DealHoleCards(ctx); err != nil {
		return fmt.Errorf("deal: %w", err)
	}
	r.node.PrintCards()

	//rounds
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		preState := game.Replay(r.log.Entries(), r.log.NumPlayers(), r.log.StartingStack())
		if preState.HandOver {
			r.node.EmitKind("round", "runner",
				"[%s] hand over (street %d, non-folded %d)",
				r.node.ID(), preState.Street, preState.NumNonFolded())
			if err := r.finishHand(ctx, preState); err != nil {
				return fmt.Errorf("finish hand: %w", err)
			}
			return nil
		}

		if err := r.runRound(ctx); err != nil {
			return fmt.Errorf("round %d: %w", r.log.RoundID(), err)
		}

		postState := game.Replay(r.log.Entries(), r.log.NumPlayers(), r.log.StartingStack())
		if postState.Street != preState.Street {
			if err := r.revealStreet(ctx, postState.Street); err != nil {
				return fmt.Errorf("reveal street %d: %w", postState.Street, err)
			}
			r.node.PrintCommunity()
		}
	}
}

// finishHand runs showdown when ≥2 seats remain, else emits uncontested winner.
func (r *Runner) finishHand(ctx context.Context, state game.State) error {
	if state.NumNonFolded() <= 1 {
		winner := -1
		for i, f := range state.Folded {
			if !f {
				winner = i
				break
			}
		}
		r.node.EmitFields("showdown", "runner",
			fmt.Sprintf("[%s] uncontested winner seat %d", r.node.ID(), winner),
			map[string]string{
				"winner": fmt.Sprintf("%d", winner),
				"reason": "uncontested",
			})
		return nil
	}
	_, err := r.node.RunShowdown(ctx, state.Folded)
	return err
}

// revealStreet fires the per-street community reveal. Showdown and Preflop
// have no community cards to reveal here (flop/turn/river only).
func (r *Runner) revealStreet(ctx context.Context, street game.Street) error {
	switch street {
	case game.StreetFlop:
		return r.node.RevealFlop(ctx)
	case game.StreetTurn:
		return r.node.RevealTurn(ctx)
	case game.StreetRiver:
		return r.node.RevealRiver(ctx)
	default:
		panic("unhandled default case")
	}
	return nil
}

// runRound drives a single round through up to MaxAttempts proposal attempts.
// On exhaustion, it co-signs an auto-fold entry for the stalling proposer.
func (r *Runner) runRound(ctx context.Context) error {
	roundID := r.log.RoundID()
	proposerSeat := r.log.ExpectedNextPlayer()
	r.node.EmitFields("round", "runner",
		fmt.Sprintf("[%d] RUN_ROUND # %d (proposer seat %d)", r.node.SeatIdx, roundID, proposerSeat),
		map[string]string{
			"round_id":      fmt.Sprintf("%d", roundID),
			"proposer_seat": fmt.Sprintf("%d", proposerSeat),
			"my_seat":       fmt.Sprintf("%d", r.node.SeatIdx),
		})

	for attempt := uint32(0); attempt < r.cfg.MaxAttempts; attempt++ {
		outcome, reason := r.runAttempt(ctx, roundID, attempt)
		r.cleanupAttempt(roundID, attempt)

		if err := ctx.Err(); err != nil {
			return err
		}

		switch outcome {
		case attemptCommitted:
			r.emit(AttemptEvent{RoundID: roundID, Attempt: attempt, Outcome: "committed"})
			if roundID > 0 {
				r.store.DeletePrefix(ephemeral.RoundPrefix(roundID - 1))
				r.store.DeletePrefix(ephemeral.AutoFoldPrefix(roundID - 1))
			}
			return nil
		case attemptTimeout:
			r.emit(AttemptEvent{RoundID: roundID, Attempt: attempt, Outcome: "timeout", Reason: reason})
		}
	}

	// Automatically emit fold once all proposer attempts are exhausted (limit
	// set by dispatch on game start). All nodes will "attest" to this fold given
	// the timeout and "auto-fold" the stalling proposer. Community will co-sign from
	// same derived log state.
	if err := r.driveAutoFold(ctx, roundID, proposerSeat); err != nil {
		r.emit(AttemptEvent{RoundID: roundID, Outcome: "auto_fold", Reason: err.Error()})
		return fmt.Errorf("auto-fold: %w", err)
	}
	r.emit(AttemptEvent{RoundID: roundID, Outcome: "auto_fold"})
	return nil
}

// runAttempt runs one attempt under a fresh per-attempt deadline.
func (r *Runner) runAttempt(ctx context.Context, roundID uint64, attempt uint32) (attemptOutcome, string) {
	attemptCtx, cancel := context.WithTimeout(ctx, r.cfg.TimeoutInterval)
	defer cancel()

	if r.currentRole() == RoleProposer {
		return r.runProposerAttempt(attemptCtx, roundID, attempt)
	}
	return r.runVerifierAttempt(attemptCtx, roundID, attempt)
}

// runProposerAttempt loops on actions until one passes local legality and
// commits, or the deadline fires. Illegal actions emit "rejected" and retry
// in-place: the store is never touched for them.
func (r *Runner) runProposerAttempt(ctx context.Context, roundID uint64, attempt uint32) (attemptOutcome, string) {
	for {
		var action game.Action
		select {
		case <-ctx.Done():
			return attemptTimeout, "no legal action before deadline"
		case action = <-r.actionCh:
		}

		entry := r.log.BuildProposal(uint8(r.node.SeatIdx), action, r.sk)
		if err := r.log.VerifyProposal(entry, r.pk); err != nil {
			r.emit(AttemptEvent{
				RoundID: roundID,
				Attempt: attempt,
				Outcome: "rejected",
				Reason:  err.Error(),
			})
			continue
		}

		data, err := gobEncode(entry)
		if err != nil {
			return attemptTimeout, fmt.Sprintf("encode proposal: %v", err)
		}
		r.store.Put(ephemeral.ProposalKey(roundID, attempt), data)

		r.log.Append(entry)
		if err := r.hostVerifyReceipt(roundID, attempt); err != nil {
			r.log.RollbackLast()
			return attemptTimeout, err.Error()
		}

		if err := r.collectVerifyReceipts(ctx, roundID, attempt); err != nil {
			r.log.RollbackLast()
			return attemptTimeout, err.Error()
		}
		return attemptCommitted, ""
	}
}

// runVerifierAttempt polls the proposer for their hosted proposal, verifies it,
// and exchanges verify receipts. Invalid proposals cause a silent wait-out so
// the abort is timeout-driven (matches the whitepaper's consistency model).
func (r *Runner) runVerifierAttempt(ctx context.Context, roundID uint64, attempt uint32) (attemptOutcome, string) {
	proposerID := r.currentProposer()
	key := ephemeral.ProposalKey(roundID, attempt)

	data, err := r.node.PollRemote(ctx, proposerID, key)
	if err != nil {
		return attemptTimeout, fmt.Sprintf("poll proposal from %s: %v", proposerID, err)
	}

	var entry game.Entry
	if err := gobDecode(data, &entry); err != nil {
		return attemptTimeout, fmt.Sprintf("decode proposal: %v", err)
	}

	proposerPK := r.node.PeerPK(proposerID)
	if err := r.log.VerifyProposal(entry, proposerPK); err != nil {
		// Silent on invalid proposals (spec); wait out the deadline.
		<-ctx.Done()
		return attemptTimeout, err.Error()
	}

	r.log.Append(entry)
	if err := r.hostVerifyReceipt(roundID, attempt); err != nil {
		r.log.RollbackLast()
		return attemptTimeout, err.Error()
	}

	if err := r.collectVerifyReceipts(ctx, roundID, attempt); err != nil {
		r.log.RollbackLast()
		return attemptTimeout, err.Error()
	}
	return attemptCommitted, ""
}

// driveAutoFold hosts this node's attestation (unless we're the target) and
// collects the others, then appends a co-signed Fold entry. Every replica runs
// this after the same K consecutive aborts and converges on the same entry.
func (r *Runner) driveAutoFold(ctx context.Context, roundID uint64, targetSeat uint8) error {
	afCtx, cancel := context.WithTimeout(ctx, r.cfg.TimeoutInterval)
	defer cancel()

	mySeat := uint8(r.node.SeatIdx)
	mySig := []byte(nil)
	if mySeat != targetSeat {
		mySig = r.log.BuildAutoFoldAttestation(roundID, targetSeat, r.sk)
		r.store.Put(ephemeral.AutoFoldKey(roundID, r.node.ID()), mySig)
		r.node.EmitKind("auto_fold", "runner",
			"[autoFold %s] round %d: hosted attestation key=%s",
			r.node.ID(), roundID, ephemeral.AutoFoldKey(roundID, r.node.ID()))
	}

	expected := r.log.ExpectedAutoFoldAttestors(targetSeat)
	r.node.EmitKind("auto_fold", "runner",
		"[autoFold %s] round %d: target=seat%d expected=%v",
		r.node.ID(), roundID, targetSeat, expected)
	if len(expected) == 0 {
		return errors.New("no eligible attestors")
	}

	cosigs := make([][]byte, len(expected))
	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)
	for i, seat := range expected {
		idx, attestorSeat := i, seat
		wg.Add(1)
		go func() {
			defer wg.Done()
			if attestorSeat == mySeat {
				cosigs[idx] = mySig
				return
			}
			attestorID := r.node.Order[attestorSeat]
			key := ephemeral.AutoFoldKey(roundID, attestorID)
			r.node.EmitKind("auto_fold", "runner", "[autoFold %s] polling %s key=%s", r.node.ID(), attestorID, key)
			data, err := r.node.PollRemote(afCtx, attestorID, key)
			if err != nil {
				r.node.EmitKind("auto_fold", "runner", "[autoFold %s] poll %s FAILED: %v", r.node.ID(), attestorID, err)
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("poll auto-fold from %s: %w", attestorID, err)
				}
				mu.Unlock()
				return
			}
			r.node.EmitKind("auto_fold", "runner", "[autoFold %s] poll %s OK (%d bytes)", r.node.ID(), attestorID, len(data))
			cosigs[idx] = data
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}

	pks := make(map[uint8]ed25519.PublicKey, len(expected))
	for _, seat := range expected {
		if pk := r.node.PeerPK(r.node.Order[seat]); pk != nil {
			pks[seat] = pk
		}
	}

	entry := game.Entry{
		RoundID:      roundID,
		PlayerID:     targetSeat,
		Action:       game.Action{Kind: game.ActionFold},
		CoSigners:    expected,
		CoSignatures: cosigs,
	}
	if err := r.log.AppendAutoFold(entry, pks); err != nil {
		return err
	}

	r.node.EmitFields("auto_fold", "runner",
		fmt.Sprintf("[autoFold %s] round %d: target seat %d folded", r.node.ID(), roundID, targetSeat),
		map[string]string{
			"round_id":    fmt.Sprintf("%d", roundID),
			"target_seat": fmt.Sprintf("%d", targetSeat),
		})
	return nil
}

func (r *Runner) currentRole() Role {
	if int(r.log.ExpectedNextPlayer()) == r.node.SeatIdx {
		return RoleProposer
	}
	return RoleVerifier
}

func (r *Runner) currentProposer() string {
	return r.node.Order[r.log.ExpectedNextPlayer()]
}

func (r *Runner) hostVerifyReceipt(roundID uint64, attempt uint32) error {
	receipt := r.log.BuildVerifyReceipt(uint8(r.node.SeatIdx), r.sk)
	data, err := gobEncode(receipt)
	if err != nil {
		return fmt.Errorf("encode verify receipt: %w", err)
	}
	r.store.Put(ephemeral.VerifyKey(roundID, attempt, r.node.ID()), data)
	return nil
}

// collectVerifyReceipts polls all other active nodes for their receipts
// concurrently. Folded seats are skipped: they can stay disconnected.
func (r *Runner) collectVerifyReceipts(ctx context.Context, roundID uint64, attempt uint32) error {
	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)

	state := game.Replay(r.log.Entries(), r.log.NumPlayers(), r.log.StartingStack())

	for seatIdx, nodeID := range r.node.Order {
		if nodeID == r.node.ID() {
			continue
		}
		if state.Folded[seatIdx] {
			continue
		}
		wg.Add(1)
		go func(nid string) {
			defer wg.Done()
			key := ephemeral.VerifyKey(roundID, attempt, nid)
			if _, err := r.node.PollRemote(ctx, nid, key); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("poll verify from %s: %w", nid, err)
				}
				mu.Unlock()
			}
		}(nodeID)
	}

	wg.Wait()
	return firstErr
}

// cleanupAttempt drops ephemeral files this node hosted for (roundID, attempt)
// and the peer receipts it fetched. Our own verify receipt is preserved so a
// slower peer can still read it; runRound bulk-deletes it on next commit.
func (r *Runner) cleanupAttempt(roundID uint64, attempt uint32) {
	r.store.Delete(ephemeral.ProposalKey(roundID, attempt))
	for _, nodeID := range r.node.Order {
		if nodeID == r.node.ID() {
			continue
		}
		r.store.Delete(ephemeral.VerifyKey(roundID, attempt, nodeID))
	}
}

func (r *Runner) emit(ev AttemptEvent) {
	select {
	case r.events <- ev:
	default:
	}

	fields := map[string]string{
		"round_id": fmt.Sprintf("%d", ev.RoundID),
		"attempt":  fmt.Sprintf("%d", ev.Attempt),
		"outcome":  ev.Outcome,
		"reason":   ev.Reason,
	}
	// On commit, the action that just landed is the tail of the log.
	// Surface its seat + kind + amount so the frontend can render who
	// did what without reading the log itself.
	if ev.Outcome == "committed" {
		entries := r.log.Entries()
		if len(entries) > 0 {
			last := entries[len(entries)-1]
			fields["seat"] = fmt.Sprintf("%d", last.PlayerID)
			fields["action_kind"] = actionKindName(last.Action.Kind)
			fields["action_amount"] = fmt.Sprintf("%d", last.Action.Amount)
		}
	}

	r.node.EmitFields("attempt", "runner",
		fmt.Sprintf("[attempt %s] round %d attempt %d: %s %s", r.node.ID(), ev.RoundID, ev.Attempt, ev.Outcome, ev.Reason),
		fields)
}

func actionKindName(k game.ActionKind) string {
	switch k {
	case game.ActionFold:
		return "fold"
	case game.ActionCheck:
		return "check"
	case game.ActionCall:
		return "call"
	case game.ActionRaise:
		return "raise"
	default:
		return "unknown"
	}
}

func gobEncode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gobDecode(data []byte, dst any) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(dst)
}
