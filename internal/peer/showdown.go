package peer

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"math/big"
	"sort"
	"sync"

	"github.com/sivepanda/p2poker/internal/card"
	"github.com/sivepanda/p2poker/internal/ephemeral"
)

// ShowdownClaim is published by every non-folded seat at showdown.
// InvExp lets verifiers strip the claimant's SRA layer off the last
// hole-relay intermediate and confirm the plaintext matches.
type ShowdownClaim struct {
	Seat   uint8
	Card1  string
	Card2  string
	InvExp []byte
}

type VerifiedClaim struct {
	Seat int
	Hand card.HandRank
}

type ShowdownResult struct {
	Winners []int
	Claims  []VerifiedClaim
}

// RunShowdown publishes our claim (if non-folded), pulls and verifies every
// other non-folded seat's claim, evaluates hands, and picks the winner(s).
func (n *Node) RunShowdown(ctx context.Context, folded []bool) (*ShowdownResult, error) {
	if n.SeatIdx >= len(folded) {
		return nil, fmt.Errorf("folded slice shorter than seat count")
	}
	if !folded[n.SeatIdx] {
		if err := n.publishShowdownClaim(); err != nil {
			return nil, fmt.Errorf("publish claim: %w", err)
		}
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		claims []VerifiedClaim
	)
	for seat, nodeID := range n.Order {
		if folded[seat] {
			continue
		}
		wg.Go(func() {
			vc, err := n.pollAndVerifyClaim(ctx, seat, nodeID)
			if err != nil {
				n.EmitKind("showdown", "showdown",
					"[%s] claim from seat %d invalid: %v", n.id, seat, err)
				return
			}
			mu.Lock()
			claims = append(claims, *vc)
			mu.Unlock()
		})
	}
	wg.Wait()

	if len(claims) == 0 {
		n.EmitKind("showdown", "showdown", "[%s] no valid claims", n.id)
		return &ShowdownResult{}, nil
	}

	sort.Slice(claims, func(i, j int) bool { return claims[i].Seat < claims[j].Seat })

	best := claims[0]
	winners := []int{best.Seat}
	for _, c := range claims[1:] {
		switch card.Compare(c.Hand, best.Hand) {
		case 1:
			best = c
			winners = []int{c.Seat}
		case 0:
			winners = append(winners, c.Seat)
		}
	}

	n.EmitFields("showdown", "showdown",
		fmt.Sprintf("[%s] winner(s) %v with %s", n.id, winners, best.Hand.Describe()),
		map[string]string{
			"winners":   fmt.Sprintf("%v", winners),
			"best_hand": best.Hand.Describe(),
			"category":  best.Hand.Category.String(),
		})
	return &ShowdownResult{Winners: winners, Claims: claims}, nil
}

func (n *Node) publishShowdownClaim() error {
	claim := ShowdownClaim{
		Seat:   uint8(n.SeatIdx),
		Card1:  n.card1,
		Card2:  n.card2,
		InvExp: n.modKey.InverseExponent.Bytes(),
	}
	data, err := encodeClaim(claim)
	if err != nil {
		return err
	}
	n.store.Put(ephemeral.ShowdownClaimKey(n.SeatIdx), data)
	n.EmitKind("showdown", "showdown",
		"[%s] published claim seat %d", n.id, n.SeatIdx)
	return nil
}

func (n *Node) pollAndVerifyClaim(ctx context.Context, seat int, nodeID string) (*VerifiedClaim, error) {
	data, err := n.fetchEphemeral(ctx, nodeID, ephemeral.ShowdownClaimKey(seat))
	if err != nil {
		return nil, fmt.Errorf("fetch claim: %w", err)
	}
	claim, err := decodeClaim(data)
	if err != nil {
		return nil, fmt.Errorf("decode claim: %w", err)
	}
	if int(claim.Seat) != seat {
		return nil, fmt.Errorf("seat mismatch: got %d, want %d", claim.Seat, seat)
	}

	invExp := new(big.Int).SetBytes(claim.InvExp)
	if err := n.verifyClaimCard(ctx, seat, 2*seat, claim.Card1, invExp); err != nil {
		return nil, fmt.Errorf("verify card1: %w", err)
	}
	if err := n.verifyClaimCard(ctx, seat, 2*seat+1, claim.Card2, invExp); err != nil {
		return nil, fmt.Errorf("verify card2: %w", err)
	}

	cards := make([]card.Card, 0, 7)
	for _, s := range []string{claim.Card1, claim.Card2} {
		c, err := card.ParseDeckString(s)
		if err != nil {
			return nil, fmt.Errorf("parse hole %q: %w", s, err)
		}
		cards = append(cards, c)
	}
	for _, s := range n.CommunityCards() {
		if s == "" {
			continue
		}
		c, err := card.ParseDeckString(s)
		if err != nil {
			return nil, fmt.Errorf("parse community %q: %w", s, err)
		}
		cards = append(cards, c)
	}
	hr, err := card.Best(cards)
	if err != nil {
		return nil, fmt.Errorf("hand eval: %w", err)
	}
	return &VerifiedClaim{Seat: seat, Hand: hr}, nil
}

// verifyClaimCard checks (last hole-relay)^InvExp == claimedPlaintext.
func (n *Node) verifyClaimCard(ctx context.Context, ownerSeat, slot int, claimed string, invExp *big.Int) error {
	ring := nonOwnerRing(len(n.Order), ownerSeat)
	if len(ring) == 0 {
		return fmt.Errorf("empty ring for slot %d", slot)
	}
	lastStripper := ring[len(ring)-1]
	lastID := n.Order[lastStripper]
	data, err := n.fetchEphemeral(ctx, lastID, ephemeral.HoleRelayKey(slot, lastStripper))
	if err != nil {
		return fmt.Errorf("fetch hole relay: %w", err)
	}
	c := new(big.Int).SetBytes(data)
	m := new(big.Int).Exp(c, invExp, n.prime)
	got := string(m.Bytes())
	if got != claimed {
		return fmt.Errorf("mismatch: got %q, claimed %q", got, claimed)
	}
	return nil
}

// fetchEphemeral reads from local store if nodeID is us, else polls remote.
func (n *Node) fetchEphemeral(ctx context.Context, nodeID, key string) ([]byte, error) {
	if nodeID == n.id {
		v, ok := n.store.Get(key)
		if !ok {
			return nil, fmt.Errorf("local key %q missing", key)
		}
		return v, nil
	}
	return n.PollRemote(ctx, nodeID, key)
}

func encodeClaim(c ShowdownClaim) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeClaim(data []byte) (ShowdownClaim, error) {
	var c ShowdownClaim
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&c); err != nil {
		return ShowdownClaim{}, err
	}
	return c, nil
}
