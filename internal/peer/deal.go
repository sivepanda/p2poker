package peer

import (
	"fmt"

	"github.com/sivepanda/p2poker/internal/crypto/deck"
)

const (
	MsgDealRelay = "deal_relay"
)

type DealRelayMessage struct {
	CardIdx     int    // Index in the FinalDeck
	CurrentData []byte // The card data as it stands after partial decryptions
	RequesterID string // Who originally asked for the card
}

// InitDealHandlers registers the sequential ring-relay logic for dealing.
func (n *Node) InitDealHandlers() {
	n.Handle(MsgDealRelay, func(msg Message) {
		req, err := Decode[DealRelayMessage](msg)
		if err != nil {
			return
		}

		// 1. Strip our layer of encryption
		decryptedData := deck.DecryptCard(req.CurrentData, n.modKey, n.prime)

		//MINE
		if req.RequesterID == n.ID() {
			if n.card1 == "" {
				n.card1 = string(decryptedData)
			} else {
				n.card2 = string(decryptedData)
			}
			return
		}

		// 2. Determine who is next in the ring
		nextIdx := (n.SeatIdx + 1) % len(n.Order)
		nextID := n.Order[nextIdx]

		// Forward the relay
		_ = n.Send(nextID, MsgDealRelay, DealRelayMessage{
			CardIdx:     req.CardIdx,
			CurrentData: decryptedData,
			RequesterID: req.RequesterID,
		})
	})
}

// RequestCards starts the ring relay for the node's two hole cards.
// This version assumes the final decryption is handled by the receiver
// logic when the relay completes the circle.
func (n *Node) RequestCards() error {
	// Go loop for: Seat 0 (0,1), Seat 1 (2,3), etc.
	start := n.SeatIdx * 2
	end := start + 2

	for cardIdx := start; cardIdx < end; cardIdx++ {
		if len(n.FinalDeck) <= cardIdx {
			return fmt.Errorf("card index %d out of bounds", cardIdx)
		}

		// Identify the next player in the ring
		nextIdx := (n.SeatIdx + 1) % len(n.Order)
		nextID := n.Order[nextIdx]

		fmt.Printf("[%s] Starting ring relay for card %d -> sending to %s\n", n.id, cardIdx, nextID)

		// Sending the raw ciphertext; your handler will strip your layer
		// when this message eventually loops back to you.
		err := n.Send(nextID, MsgDealRelay, DealRelayMessage{
			CardIdx:     cardIdx,
			CurrentData: n.FinalDeck[cardIdx],
			RequesterID: n.id,
		})

		if err != nil {
			return fmt.Errorf("failed to start relay for card %d: %w", cardIdx, err)
		}
	}

	return nil
}

func (n *Node) NoCardsYet() bool {
	return n.card1 == "" || n.card2 == ""
}

// HoleCards returns this node's two hole cards. Either may be empty if the
// deal relay has not yet completed.
func (n *Node) HoleCards() (string, string) {
	return n.card1, n.card2
}

func (n *Node) PrintCards() {
	if n.NoCardsYet() {
		fmt.Printf("[%s] Hole Cards: [Waiting for deal...]\n", n.id)
		return
	}

	fmt.Println("--------------------------")
	fmt.Printf("[%s] YOUR HAND\n", n.id)
	fmt.Printf("Card 1: %s\n", n.card1)
	fmt.Printf("Card 2: %s\n", n.card2)
	fmt.Println("--------------------------")
}
