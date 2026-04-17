package peer

import (
	"fmt"

	"github.com/sivepanda/p2poker/internal/crypto/deck"
)

const (
	MsgCommunityRelay  = "community_relay"
	MsgCommunityReveal = "community_reveal"
)

type CommunityRelayMessage struct {
	CardIdx     int    // Index in the FinalDeck
	CurrentData []byte // Partially decrypted card data
	InitiatorID string // Who started the relay (seat 0)
}

type CommunityRevealMessage struct {
	CardIdx   int    // Index in the FinalDeck
	Plaintext string // Fully decrypted card value
}

// InitCommunityHandlers registers handlers for community card decryption.
func (n *Node) InitCommunityHandlers() {
	n.Handle(MsgCommunityRelay, func(msg Message) {
		req, err := Decode[CommunityRelayMessage](msg)
		if err != nil {
			return
		}

		decryptedData := deck.DecryptCard(req.CurrentData, n.modKey, n.prime)

		// If we're the last in the ring (message came back to initiator),
		// all layers are stripped — broadcast the plaintext.
		if req.InitiatorID == n.ID() {
			plaintext := string(decryptedData)
			n.appendCommunity(plaintext)

			_ = n.Broadcast(MsgCommunityReveal, CommunityRevealMessage{
				CardIdx:   req.CardIdx,
				Plaintext: plaintext,
			})
			return
		}

		// Forward to next player in the ring.
		nextIdx := (n.SeatIdx + 1) % len(n.Order)
		nextID := n.Order[nextIdx]

		_ = n.Send(nextID, MsgCommunityRelay, CommunityRelayMessage{
			CardIdx:     req.CardIdx,
			CurrentData: decryptedData,
			InitiatorID: req.InitiatorID,
		})
	})

	n.Handle(MsgCommunityReveal, func(msg Message) {
		reveal, err := Decode[CommunityRevealMessage](msg)
		if err != nil {
			return
		}

		n.appendCommunity(reveal.Plaintext)
	})
}

// RevealCommunityCards starts the ring relay for the given card indices.
// Only the leader (seat 0) should call this.
func (n *Node) RevealCommunityCards(indices []int) error {
	if n.SeatIdx != 0 {
		return fmt.Errorf("only seat 0 initiates community reveals")
	}

	for _, cardIdx := range indices {
		if cardIdx >= len(n.FinalDeck) {
			return fmt.Errorf("card index %d out of bounds", cardIdx)
		}

		nextIdx := (n.SeatIdx + 1) % len(n.Order)
		nextID := n.Order[nextIdx]

		fmt.Printf("[%s] Starting community relay for card %d -> sending to %s\n", n.id, cardIdx, nextID)

		err := n.Send(nextID, MsgCommunityRelay, CommunityRelayMessage{
			CardIdx:     cardIdx,
			CurrentData: n.FinalDeck[cardIdx],
			InitiatorID: n.id,
		})
		if err != nil {
			return fmt.Errorf("failed to start community relay for card %d: %w", cardIdx, err)
		}
	}

	return nil
}

// communityBase returns the first deck index used for community cards.
// Hole cards occupy indices [0, 2*numPlayers).
func (n *Node) communityBase() int {
	return len(n.Order) * 2
}

// RevealFlop reveals the 3 flop cards.
func (n *Node) RevealFlop() error {
	base := n.communityBase()
	return n.RevealCommunityCards([]int{base, base + 1, base + 2})
}

// RevealTurn reveals the turn card.
func (n *Node) RevealTurn() error {
	base := n.communityBase()
	return n.RevealCommunityCards([]int{base + 3})
}

// RevealRiver reveals the river card.
func (n *Node) RevealRiver() error {
	base := n.communityBase()
	return n.RevealCommunityCards([]int{base + 4})
}

func (n *Node) appendCommunity(card string) {
	n.communityMu.Lock()
	defer n.communityMu.Unlock()
	n.community = append(n.community, card)
}

// CommunityCards returns the currently revealed community cards.
func (n *Node) CommunityCards() []string {
	n.communityMu.RLock()
	defer n.communityMu.RUnlock()
	out := make([]string, len(n.community))
	copy(out, n.community)
	return out
}

// CommunityCount returns how many community cards have been revealed so far.
func (n *Node) CommunityCount() int {
	n.communityMu.RLock()
	defer n.communityMu.RUnlock()
	return len(n.community)
}

// PrintCommunity prints the revealed community cards.
func (n *Node) PrintCommunity() {
	n.communityMu.RLock()
	defer n.communityMu.RUnlock()

	if len(n.community) == 0 {
		fmt.Printf("[%s] Community: [none yet]\n", n.id)
		return
	}

	fmt.Println("--------------------------")
	fmt.Printf("[%s] COMMUNITY CARDS\n", n.id)
	for i, c := range n.community {
		fmt.Printf("  Card %d: %s\n", i+1, c)
	}
	fmt.Println("--------------------------")
}
