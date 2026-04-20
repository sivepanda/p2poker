package peer

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"

	"github.com/sivepanda/p2poker/internal/crypto/deck"
	"github.com/sivepanda/p2poker/internal/ephemeral"
)

// RunShuffle drives the pull-based shuffle ring. Every seat calls it.
// Seat 0 starts; each subsequent seat polls the prior step, shuffles +
// encrypts, hosts at its seat. Non-last seats then poll the last seat
// for the final deck.
func (n *Node) RunShuffle(ctx context.Context) error {
	if len(n.Order) < 2 {
		return fmt.Errorf("not enough players to shuffle")
	}

	var d [][]byte
	if n.SeatIdx == 0 {
		n.EmitKind("shuffle", "shuffle", "STARTING SHUFFLE")
		d = buildOrderedDeck()
	} else {
		prevSeat := n.SeatIdx - 1
		prevID := n.Order[prevSeat]
		n.EmitKind("shuffle", "shuffle",
			"[%s] polling %s for shuffle step %d", n.id, prevID, prevSeat)
		data, err := n.PollRemote(ctx, prevID, ephemeral.ShuffleStepKey(prevSeat))
		if err != nil {
			return fmt.Errorf("poll shuffle step %d: %w", prevSeat, err)
		}
		d, err = decodeDeck(data)
		if err != nil {
			return fmt.Errorf("decode shuffle step %d: %w", prevSeat, err)
		}
	}

	n.EmitKind("shuffle", "shuffle", "[%s] SHUFFLING DECK...", n.id)
	if err := deck.Shuffle(d); err != nil {
		return fmt.Errorf("shuffle: %w", err)
	}
	d = deck.EncryptDeck(d, n.modKey, n.prime)

	encoded, err := encodeDeck(d)
	if err != nil {
		return fmt.Errorf("encode shuffle step: %w", err)
	}
	n.store.Put(ephemeral.ShuffleStepKey(n.SeatIdx), encoded)

	lastSeat := len(n.Order) - 1
	if n.SeatIdx == lastSeat {
		n.FinalDeck = d
		n.EmitKind("shuffle", "shuffle", "[%s] FINAL DECK COMPUTED", n.id)
		return nil
	}

	lastID := n.Order[lastSeat]
	n.EmitKind("shuffle", "shuffle",
		"[%s] polling %s for final deck", n.id, lastID)
	data, err := n.PollRemote(ctx, lastID, ephemeral.ShuffleStepKey(lastSeat))
	if err != nil {
		return fmt.Errorf("poll final deck: %w", err)
	}
	final, err := decodeDeck(data)
	if err != nil {
		return fmt.Errorf("decode final deck: %w", err)
	}
	n.FinalDeck = final
	n.EmitKind("shuffle", "shuffle", "[%s] FINAL DECK RECEIVED", n.id)
	return nil
}

// buildOrderedDeck returns 52 cards as 1-based decimal strings (1..52).
// 1-based so SRA never sees the zero element.
func buildOrderedDeck() [][]byte {
	d := make([][]byte, 52)
	for i := range d {
		d[i] = fmt.Appendf(nil, "%d", i+1)
	}
	return d
}

func encodeDeck(d [][]byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeDeck(data []byte) ([][]byte, error) {
	var d [][]byte
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&d); err != nil {
		return nil, err
	}
	return d, nil
}
