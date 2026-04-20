package peer

import (
	"context"
	"fmt"
	"sync"

	"github.com/sivepanda/p2poker/internal/crypto/deck"
	"github.com/sivepanda/p2poker/internal/ephemeral"
)

// DealHoleCards runs the pull-based hole deal across all slots in parallel.
func (n *Node) DealHoleCards(ctx context.Context) error {
	numSlots := len(n.Order) * 2
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for slot := range numSlots {
		wg.Go(func() {
			if err := n.dealHoleSlot(ctx, slot); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return firstErr
}

// dealHoleSlot strips one slot through the non-owner ring; owner pulls last.
func (n *Node) dealHoleSlot(ctx context.Context, slot int) error {
	if slot < 0 || slot >= len(n.FinalDeck) {
		return fmt.Errorf("slot %d out of bounds", slot)
	}
	ownerSeat := slot / 2
	if ownerSeat >= len(n.Order) {
		return fmt.Errorf("slot %d has no owner seat", slot)
	}
	ring := nonOwnerRing(len(n.Order), ownerSeat)
	if len(ring) == 0 {
		return fmt.Errorf("empty ring for slot %d", slot)
	}

	if n.SeatIdx == ownerSeat {
		lastStripper := ring[len(ring)-1]
		lastID := n.Order[lastStripper]
		key := ephemeral.HoleRelayKey(slot, lastStripper)
		data, err := n.PollRemote(ctx, lastID, key)
		if err != nil {
			return fmt.Errorf("poll hole relay (slot %d): %w", slot, err)
		}
		plaintext := deck.DecryptCard(data, n.modKey, n.prime)
		n.setHole(slot, string(plaintext))
		return nil
	}

	ringIdx := ringPosition(ring, n.SeatIdx)
	if ringIdx < 0 {
		return fmt.Errorf("seat %d not in ring for slot %d", n.SeatIdx, slot)
	}

	var layered []byte
	if ringIdx == 0 {
		layered = n.FinalDeck[slot]
	} else {
		prevSeat := ring[ringIdx-1]
		prevID := n.Order[prevSeat]
		data, err := n.PollRemote(ctx, prevID, ephemeral.HoleRelayKey(slot, prevSeat))
		if err != nil {
			return fmt.Errorf("poll hole relay (slot %d, stripper %d): %w", slot, prevSeat, err)
		}
		layered = data
	}
	stripped := deck.DecryptCard(layered, n.modKey, n.prime)
	n.store.Put(ephemeral.HoleRelayKey(slot, n.SeatIdx), stripped)
	return nil
}

func nonOwnerRing(numPlayers, ownerSeat int) []int {
	out := make([]int, 0, numPlayers-1)
	for i := range numPlayers {
		if i != ownerSeat {
			out = append(out, i)
		}
	}
	return out
}

func ringPosition(ring []int, seat int) int {
	for i, s := range ring {
		if s == seat {
			return i
		}
	}
	return -1
}

func (n *Node) NoCardsYet() bool {
	return n.card1 == "" || n.card2 == ""
}

func (n *Node) HoleCards() (string, string) {
	return n.card1, n.card2
}

func (n *Node) PrintCards() {
	if n.NoCardsYet() {
		n.EmitKind("deal", "deal", "[%s] Hole Cards: [Waiting for deal...]", n.id)
		return
	}
	msg := fmt.Sprintf("--------------------------\n[%s] YOUR HAND\nCard 1: %s\nCard 2: %s\n--------------------------",
		n.id, n.card1, n.card2)
	n.EmitCards("deal", "deal", msg, map[string]string{
		"hole1": n.card1,
		"hole2": n.card2,
	})
}

// setHole writes plaintext for our slot. 2*owner is card1; 2*owner+1 is card2.
func (n *Node) setHole(slot int, card string) {
	if slot%2 == 0 {
		n.card1 = card
	} else {
		n.card2 = card
	}
}
