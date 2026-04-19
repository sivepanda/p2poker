package peer

import (
	"context"
	"fmt"
	"strings"

	"github.com/sivepanda/p2poker/internal/crypto/deck"
	"github.com/sivepanda/p2poker/internal/ephemeral"
)

// RevealCommunityCard participates in the pull-based ring decryption for one
// community card. Every peer must call this for each card to be revealed.
//
// Seat 0 strips its own SRA layer from FinalDeck[cardIdx] and hosts the
// result. Each subsequent seat polls the prior seat's relay key, strips its
// layer, and hosts. The last seat additionally hosts the plaintext at
// CommunityCardKey. Every peer (including seat 0) polls the plaintext key
// and records the card locally.
func (n *Node) RevealCommunityCard(ctx context.Context, cardIdx int) error {
	if cardIdx < 0 || cardIdx >= len(n.FinalDeck) {
		return fmt.Errorf("card index %d out of bounds", cardIdx)
	}

	var layered []byte
	if n.SeatIdx == 0 {
		layered = n.FinalDeck[cardIdx]
	} else {
		prevSeat := n.SeatIdx - 1
		prevID := n.Order[prevSeat]
		key := ephemeral.CommunityRelayKey(cardIdx, prevSeat)
		n.EmitKind("community", "community",
			"[%s] polling %s for community relay (card %d, seat %d)",
			n.id, prevID, cardIdx, prevSeat)
		data, err := n.PollRemote(ctx, prevID, key)
		if err != nil {
			return fmt.Errorf("poll community relay (card %d, seat %d): %w", cardIdx, prevSeat, err)
		}
		layered = data
	}

	stripped := deck.DecryptCard(layered, n.modKey, n.prime)
	n.store.Put(ephemeral.CommunityRelayKey(cardIdx, n.SeatIdx), stripped)
	n.EmitKind("community", "community",
		"[%s] hosted community relay for card %d at seat %d",
		n.id, cardIdx, n.SeatIdx)

	lastSeat := len(n.Order) - 1
	if n.SeatIdx == lastSeat {
		n.store.Put(ephemeral.CommunityCardKey(cardIdx), stripped)
		n.setCommunity(cardIdx, string(stripped))
		return nil
	}

	lastID := n.Order[lastSeat]
	plaintext, err := n.PollRemote(ctx, lastID, ephemeral.CommunityCardKey(cardIdx))
	if err != nil {
		return fmt.Errorf("poll community card %d: %w", cardIdx, err)
	}
	n.setCommunity(cardIdx, string(plaintext))
	return nil
}

// communityBase returns the first deck index used for community cards.
// Hole cards occupy indices [0, 2*numPlayers).
func (n *Node) communityBase() int {
	return len(n.Order) * 2
}

// RevealFlop participates in the flop reveal (3 cards) on this node.
func (n *Node) RevealFlop(ctx context.Context) error {
	base := n.communityBase()
	for _, idx := range []int{base, base + 1, base + 2} {
		if err := n.RevealCommunityCard(ctx, idx); err != nil {
			return err
		}
	}
	return nil
}

// RevealTurn participates in the turn reveal (1 card) on this node.
func (n *Node) RevealTurn(ctx context.Context) error {
	return n.RevealCommunityCard(ctx, n.communityBase()+3)
}

// RevealRiver participates in the river reveal (1 card) on this node.
func (n *Node) RevealRiver(ctx context.Context) error {
	return n.RevealCommunityCard(ctx, n.communityBase()+4)
}

// setCommunity records a card at its board slot (cardIdx - communityBase) and
// emits a typed CommunityCards event over RPC. Idempotent: re-setting the same
// slot is a no-op.
func (n *Node) setCommunity(cardIdx int, card string) {
	boardIdx := cardIdx - n.communityBase()

	n.communityMu.Lock()
	for len(n.community) <= boardIdx {
		n.community = append(n.community, "")
	}
	if n.community[boardIdx] != "" {
		n.communityMu.Unlock()
		return
	}
	n.community[boardIdx] = card
	snapshot := make([]string, len(n.community))
	copy(snapshot, n.community)
	n.communityMu.Unlock()

	// Emit the contiguous filled prefix only — the RPC translator iterates
	// card1..cardN until the first missing key, so gaps would hide later
	// reveals.
	cards := make(map[string]string)
	filled := 0
	for i, c := range snapshot {
		if c == "" {
			break
		}
		cards[fmt.Sprintf("card%d", i+1)] = c
		filled++
	}
	n.EmitCards("community", "community",
		fmt.Sprintf("[%s] community card revealed: %s (slot %d, total %d)",
			n.id, card, boardIdx+1, filled),
		cards)
}

// CommunityCards returns the currently revealed community cards. Empty
// strings denote not-yet-revealed slots.
func (n *Node) CommunityCards() []string {
	n.communityMu.RLock()
	defer n.communityMu.RUnlock()
	out := make([]string, len(n.community))
	copy(out, n.community)
	return out
}

// CommunityCount returns how many community slots have been filled in.
func (n *Node) CommunityCount() int {
	n.communityMu.RLock()
	defer n.communityMu.RUnlock()
	count := 0
	for _, c := range n.community {
		if c != "" {
			count++
		}
	}
	return count
}

// PrintCommunity emits the current board state as a typed RPC event.
func (n *Node) PrintCommunity() {
	n.communityMu.RLock()
	snapshot := make([]string, len(n.community))
	copy(snapshot, n.community)
	n.communityMu.RUnlock()

	if len(snapshot) == 0 {
		n.EmitKind("community", "community", "[%s] Community: [none yet]", n.id)
		return
	}

	var buf strings.Builder
	buf.WriteString("--------------------------\n")
	_, err := fmt.Fprintf(&buf, "[%s] COMMUNITY CARDS\n", n.id)
	if err != nil {
		return
	}
	cards := make(map[string]string)
	for i, c := range snapshot {
		if c == "" {
			break
		}
		_, err := fmt.Fprintf(&buf, "  Card %d: %s\n", i+1, c)
		if err != nil {
			return
		}
		cards[fmt.Sprintf("card%d", i+1)] = c
	}
	buf.WriteString("--------------------------")
	n.EmitCards("community", "community", buf.String(), cards)
}
