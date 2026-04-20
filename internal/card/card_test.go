package card

import "testing"

func TestIntRoundTrip(t *testing.T) {
	for i := range 52 {
		c, err := FromInt(i)
		if err != nil {
			t.Fatalf("FromInt(%d): %v", i, err)
		}
		if got := c.Int(); got != i {
			t.Fatalf("round trip: %d -> %v -> %d", i, c, got)
		}
	}
}

func TestFromIntOutOfRange(t *testing.T) {
	for _, i := range []int{-1, 52, 100} {
		if _, err := FromInt(i); err == nil {
			t.Fatalf("FromInt(%d) expected error", i)
		}
	}
}

func TestParseDeckString(t *testing.T) {
	cases := []struct {
		s    string
		suit Suit
		rank Rank
	}{
		{"1", Clubs, Two},
		{"13", Clubs, Ace},
		{"14", Diamonds, Two},
		{"26", Diamonds, Ace},
		{"27", Hearts, Two},
		{"52", Spades, Ace},
	}
	for _, tc := range cases {
		c, err := ParseDeckString(tc.s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.s, err)
		}
		if c.Suit != tc.suit || c.Rank != tc.rank {
			t.Fatalf("Parse(%q) = %v, want %s%s", tc.s, c, tc.rank.Short(), tc.suit)
		}
		if got := c.DeckString(); got != tc.s {
			t.Fatalf("DeckString round trip: %q -> %v -> %q", tc.s, c, got)
		}
	}
}

func TestShortAndPretty(t *testing.T) {
	c := Card{Suit: Spades, Rank: Ace}
	if got := c.Short(); got != "AS" {
		t.Fatalf("Short = %q, want AS", got)
	}
	if got := c.Pretty(); got != "A\u2660" {
		t.Fatalf("Pretty = %q", got)
	}
}
