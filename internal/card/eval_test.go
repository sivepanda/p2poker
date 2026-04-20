package card

import (
	"fmt"
	"strings"
	"testing"
)

func mustCards(t *testing.T, shorts ...string) []Card {
	t.Helper()
	out := make([]Card, len(shorts))
	for i, s := range shorts {
		c, err := parseShort(s)
		if err != nil {
			t.Fatalf("parseShort(%q): %v", s, err)
		}
		out[i] = c
	}
	return out
}

// parseShort accepts inputs like "AS", "TC", "2H".
func parseShort(s string) (Card, error) {
	if len(s) != 2 {
		return Card{}, fmt.Errorf("bad len %q", s)
	}
	rankCh, suitCh := s[0], s[1]
	const ranks = "23456789TJQKA"
	rIdx := strings.IndexByte(ranks, rankCh)
	if rIdx < 0 {
		return Card{}, fmt.Errorf("bad rank %q", s)
	}
	var suit Suit
	switch suitCh {
	case 'C':
		suit = Clubs
	case 'D':
		suit = Diamonds
	case 'H':
		suit = Hearts
	case 'S':
		suit = Spades
	default:
		return Card{}, fmt.Errorf("bad suit %q", s)
	}
	return Card{Suit: suit, Rank: Rank(rIdx)}, nil
}

func TestRank5Categories(t *testing.T) {
	cases := []struct {
		name string
		hand []string
		want Category
	}{
		{"royal flush", []string{"AS", "KS", "QS", "JS", "TS"}, StraightFlush},
		{"straight flush", []string{"9H", "8H", "7H", "6H", "5H"}, StraightFlush},
		{"wheel straight flush", []string{"AC", "2C", "3C", "4C", "5C"}, StraightFlush},
		{"four of a kind", []string{"AS", "AC", "AD", "AH", "2S"}, FourOfAKind},
		{"full house", []string{"KS", "KC", "KD", "2H", "2S"}, FullHouse},
		{"flush", []string{"AS", "JS", "9S", "5S", "2S"}, Flush},
		{"straight", []string{"9H", "8C", "7D", "6S", "5H"}, Straight},
		{"wheel straight", []string{"AC", "2D", "3H", "4S", "5C"}, Straight},
		{"three of a kind", []string{"7S", "7C", "7D", "KH", "2S"}, ThreeOfAKind},
		{"two pair", []string{"AS", "AC", "KD", "KH", "2S"}, TwoPair},
		{"one pair", []string{"AS", "AC", "KD", "9H", "2S"}, OnePair},
		{"high card", []string{"AS", "JC", "9D", "5H", "2S"}, HighCard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := mustCards(t, tc.hand...)
			var arr [5]Card
			copy(arr[:], cs)
			got := Rank5(arr)
			if got.Category != tc.want {
				t.Fatalf("category = %v, want %v (%s)", got.Category, tc.want, got.Describe())
			}
		})
	}
}

func TestWheelStraightHighIsFive(t *testing.T) {
	cs := mustCards(t, "AC", "2D", "3H", "4S", "5C")
	var arr [5]Card
	copy(arr[:], cs)
	got := Rank5(arr)
	if got.Category != Straight {
		t.Fatalf("category = %v", got.Category)
	}
	if got.Tiebreakers[0] != Five {
		t.Fatalf("wheel high should be Five, got %v", got.Tiebreakers[0])
	}
}

func TestCategoryOrdering(t *testing.T) {
	hands := [][]string{
		{"AS", "JC", "9D", "5H", "2S"}, // high card
		{"AS", "AC", "KD", "9H", "2S"}, // pair
		{"AS", "AC", "KD", "KH", "2S"}, // two pair
		{"7S", "7C", "7D", "KH", "2S"}, // trips
		{"9H", "8C", "7D", "6S", "5H"}, // straight
		{"AS", "JS", "9S", "5S", "2S"}, // flush
		{"KS", "KC", "KD", "2H", "2S"}, // full house
		{"AS", "AC", "AD", "AH", "2S"}, // quads
		{"9H", "8H", "7H", "6H", "5H"}, // straight flush
	}
	prev := HandRank{}
	for i, h := range hands {
		cs := mustCards(t, h...)
		var arr [5]Card
		copy(arr[:], cs)
		got := Rank5(arr)
		if i > 0 && Compare(got, prev) <= 0 {
			t.Fatalf("hand %d (%v) should beat prev (%v)", i, got.Category, prev.Category)
		}
		prev = got
	}
}

func TestBest7PicksTopHand(t *testing.T) {
	// Hole AC AS, board AD KH 7C 7D 2S -> Aces full of Sevens (full house).
	cs := mustCards(t, "AC", "AS", "AD", "KH", "7C", "7D", "2S")
	got, err := Best(cs)
	if err != nil {
		t.Fatalf("Best: %v", err)
	}
	if got.Category != FullHouse {
		t.Fatalf("category = %v (%s)", got.Category, got.Describe())
	}
	if got.Tiebreakers[0] != Ace || got.Tiebreakers[1] != Seven {
		t.Fatalf("tiebreakers = %v", got.Tiebreakers)
	}
}

func TestBest7BeatsPair(t *testing.T) {
	// Hole 9H 8H, board 7H 6H 5H 2C 2D -> straight flush, not full house of 2s.
	cs := mustCards(t, "9H", "8H", "7H", "6H", "5H", "2C", "2D")
	got, err := Best(cs)
	if err != nil {
		t.Fatalf("Best: %v", err)
	}
	if got.Category != StraightFlush {
		t.Fatalf("category = %v (%s)", got.Category, got.Describe())
	}
	if got.Tiebreakers[0] != Nine {
		t.Fatalf("high = %v", got.Tiebreakers[0])
	}
}

func TestBestRejectsDuplicates(t *testing.T) {
	cs := mustCards(t, "AC", "AC", "AD", "KH", "7C", "7D", "2S")
	if _, err := Best(cs); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestDescribeRoyalFlush(t *testing.T) {
	cs := mustCards(t, "AS", "KS", "QS", "JS", "TS")
	var arr [5]Card
	copy(arr[:], cs)
	got := Rank5(arr)
	if d := got.Describe(); d != "Royal Flush" {
		t.Fatalf("describe = %q", d)
	}
}
