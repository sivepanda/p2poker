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

// Runner orchestrates the propose/verify/commit lifecycle for one node.
type Runner struct {
	node  *peer.Node
	store *ephemeral.Store
	log   *game.Log

	sk ed25519.PrivateKey
	pk ed25519.PublicKey

	actionCh chan game.Action
}

// New creates a Runner from the game start data.
func New(
	node *peer.Node,
	store *ephemeral.Store,
	sk ed25519.PrivateKey,
	pk ed25519.PublicKey,
) *Runner {
	gameLog := game.NewLog(uint8(len(node.Order)), uint64(node.Money()))

	return &Runner{
		node:     node,
		store:    store,
		log:      gameLog,
		sk:       sk,
		pk:       pk,
		actionCh: make(chan game.Action, 1),
	}
}

// Log returns the runner's game log.
func (r *Runner) Log() *game.Log {
	return r.log
}

// SubmitAction queues an action for when it is this node's turn.
func (r *Runner) SubmitAction(action game.Action) error {
	if r.currentRole() != RoleProposer {
		return errors.New("not our turn")
	}
	select {
	case r.actionCh <- action:
		return nil
	default:
		return errors.New("action already pending")
	}
}

// Run starts the round lifecycle loop. Blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	for r.node.Started == false {
		time.Sleep(200 * time.Millisecond)
	}
	//before rounds
	err := r.node.StartShuffle()
	if err != nil {
		return err
	}

	for r.node.FinalDeck == nil {
		time.Sleep(200 * time.Millisecond)
	}

	err = r.node.RequestCards()
	for r.node.NoCardsYet() {
		time.Sleep(200 * time.Millisecond)
	}
	r.node.PrintCards()

	// Seat 0 initiates the flop reveal once all hole cards are dealt.
	if r.node.SeatIdx == 0 {
		if err := r.node.RevealFlop(); err != nil {
			return fmt.Errorf("reveal flop: %w", err)
		}
	}
	for r.node.CommunityCount() < 3 {
		time.Sleep(200 * time.Millisecond)
	}
	r.node.PrintCommunity()

	//rounds
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.runRound(ctx); err != nil {
			return fmt.Errorf("round %d: %w", r.log.RoundID(), err)
		}
	}
}

func (r *Runner) runRound(ctx context.Context) error {
	fmt.Printf("[%d] RUN_ROUND # %d\n", r.node.SeatIdx, r.log.RoundID())
	roundID := r.log.RoundID()
	role := r.currentRole()

	// Phase 1: obtain the proposal based on our role.
	entry, err := r.obtainProposal(ctx, roundID, role)
	if err != nil {
		return err
	}

	// Phase 2: verify the proposal against the proposer's registered pk.
	proposerPK := r.node.PeerPK(r.currentProposer())
	if err := r.log.VerifyProposal(entry, proposerPK); err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	// Phase 3: append to log and host our verify receipt.
	r.log.Append(entry)
	if err := r.hostVerifyReceipt(roundID); err != nil {
		return err
	}

	// Phase 4: collect all other nodes' verify receipts.
	if err := r.collectVerifyReceipts(ctx, roundID); err != nil {
		return err
	}

	// Phase 5: cleanup ephemeral files for this round.
	// We keep our own verify receipt for one extra round so slower peers can
	// still poll it — a node finishing collectVerifyReceipts doesn't imply
	// others have finished polling ours.
	r.cleanup(roundID)
	if roundID > 0 {
		r.store.Delete(ephemeral.VerifyKey(roundID-1, r.node.ID()))
	}
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

func (r *Runner) obtainProposal(ctx context.Context, roundID uint64, role Role) (game.Entry, error) {
	switch role {
	case RoleProposer:
		return r.buildAndHostProposal(ctx, roundID)
	case RoleVerifier:
		return r.pollForProposal(ctx, roundID)
	default:
		return game.Entry{}, fmt.Errorf("unknown role %d", role)
	}
}

// buildAndHostProposal waits for an action, signs it, and hosts it.
func (r *Runner) buildAndHostProposal(ctx context.Context, roundID uint64) (game.Entry, error) {
	var action game.Action
	select {
	case <-ctx.Done():
		return game.Entry{}, ctx.Err()
	case action = <-r.actionCh:
	}

	entry := r.log.BuildProposal(uint8(r.node.SeatIdx), action, r.sk)

	data, err := gobEncode(entry)
	if err != nil {
		return game.Entry{}, fmt.Errorf("encode proposal: %w", err)
	}
	r.store.Put(ephemeral.ProposalKey(roundID), data)

	return entry, nil
}

// pollForProposal polls the proposer node until the proposal file appears.
func (r *Runner) pollForProposal(ctx context.Context, roundID uint64) (game.Entry, error) {
	proposerID := r.currentProposer()
	key := ephemeral.ProposalKey(roundID)

	data, err := r.node.PollRemote(ctx, proposerID, key)
	if err != nil {
		return game.Entry{}, fmt.Errorf("poll proposal from %s: %w", proposerID, err)
	}

	var entry game.Entry
	if err := gobDecode(data, &entry); err != nil {
		return game.Entry{}, fmt.Errorf("decode proposal: %w", err)
	}
	return entry, nil
}

func (r *Runner) hostVerifyReceipt(roundID uint64) error {
	receipt := r.log.BuildVerifyReceipt(uint8(r.node.SeatIdx), r.sk)
	data, err := gobEncode(receipt)
	if err != nil {
		return fmt.Errorf("encode verify receipt: %w", err)
	}
	r.store.Put(ephemeral.VerifyKey(roundID, r.node.ID()), data)
	return nil
}

// collectVerifyReceipts polls all other active nodes for their receipts
// concurrently. Folded seats are skipped and they can be ignored (and thus allowed to disconnect)
func (r *Runner) collectVerifyReceipts(ctx context.Context, roundID uint64) error {
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
			key := ephemeral.VerifyKey(roundID, nid)
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

func (r *Runner) cleanup(roundID uint64) {
	r.store.Delete(ephemeral.ProposalKey(roundID))
	for _, nodeID := range r.node.Order {
		if nodeID == r.node.ID() {
			continue
		}
		r.store.Delete(ephemeral.VerifyKey(roundID, nodeID))
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
