package peer

import (
	"fmt"

	"github.com/sivepanda/p2poker/internal/crypto/deck"
)

const (
	MsgShuffleReq = "shuffle_req"
	MsgFinalDeck  = "final_deck"
)

type ShuffleRequest struct {
	Deck [][]byte
	From string
}

type ShuffleResponse struct {
	OK bool
}

type FinalDeckMessage struct {
	Deck [][]byte
}

// InitShuffleHandlers registers the message the shuffle round-robin.
// Call once after Connect().
func (n *Node) InitShuffleHandlers() {

	// Handle shuffle request (relay step)
	n.Handle(MsgShuffleReq, func(msg Message) {
		fmt.Printf("[%s] SHUFFLING DECK...\n", n.id)
		req, err := Decode[ShuffleRequest](msg)
		if err != nil {
			return
		}

		// Shuffle + encrypt
		d := req.Deck
		_ = deck.Shuffle(d)
		d = deck.EncryptDeck(d, n.modKey, n.prime)

		// If LAST node → broadcast final deck
		if n.SeatIdx == len(n.Order)-1 {
			_ = n.Broadcast(MsgFinalDeck, FinalDeckMessage{
				Deck: d,
			})
			n.FinalDeck = d
			fmt.Printf("[%s] FINAL DECK RECIEVED\n", n.id)
			return
		}

		// Otherwise → forward to next node
		nextID := n.Order[n.SeatIdx+1]

		_ = n.Send(nextID, MsgShuffleReq, ShuffleRequest{
			Deck: d,
			From: n.ID(),
		})
	})

	// Handle final deck broadcast
	n.Handle(MsgFinalDeck, func(msg Message) {
		fmt.Printf("[%s] FINAL DECK RECIEVED\n", n.id)
		final, err := Decode[FinalDeckMessage](msg)
		if err != nil {
			return
		}

		n.FinalDeck = final.Deck
	})
}

func (n *Node) StartShuffle() error {
	// Only leader starts the shuffle
	if len(n.Order) == 0 || n.SeatIdx != 0 {
		return nil
	}
	if len(n.Order) < 2 {
		return fmt.Errorf("not enough players to shuffle")
	}

	fmt.Printf("STARTING SHUFFLE\n")
	fmt.Printf("[%s] SHUFFLING DECK...\n", n.id)
	d := buildOrderedDeck()
	_ = deck.Shuffle(d)
	d = deck.EncryptDeck(d, n.modKey, n.prime)

	return n.Send(n.Order[1], MsgShuffleReq, ShuffleRequest{
		Deck: d,
		From: n.ID(),
	})
}

// buildOrderedDeck returns 52 cards as [][]byte with values "1"..."52".
// 1-based so no card is the zero element, which SRA requires.
func buildOrderedDeck() [][]byte {
	d := make([][]byte, 52)
	for i := range d {
		d[i] = []byte(fmt.Sprintf("%d", i+1))
	}
	return d
}
