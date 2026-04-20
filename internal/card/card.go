// Package card maps the deck's [0,52) integer encoding to typed
// suits/ranks and evaluates Texas Hold'em hand strength.
//
// Deck convention: i = suit*13 + rank, with suit 0=Clubs..3=Spades and
// rank 0=Two..12=Ace. The wire format used by the rest of the codebase
// is the decimal string of the integer (e.g. "0", "33").
package card

import (
	"fmt"
	"strconv"
)

type Suit uint8

const (
	Clubs Suit = iota
	Diamonds
	Hearts
	Spades
)

func (s Suit) String() string {
	switch s {
	case Clubs:
		return "C"
	case Diamonds:
		return "D"
	case Hearts:
		return "H"
	case Spades:
		return "S"
	}
	return "?"
}

func (s Suit) Symbol() string {
	switch s {
	case Clubs:
		return "\u2663"
	case Diamonds:
		return "\u2666"
	case Hearts:
		return "\u2665"
	case Spades:
		return "\u2660"
	}
	return "?"
}

func (s Suit) Name() string {
	switch s {
	case Clubs:
		return "Clubs"
	case Diamonds:
		return "Diamonds"
	case Hearts:
		return "Hearts"
	case Spades:
		return "Spades"
	}
	return "?"
}

type Rank uint8

const (
	Two Rank = iota
	Three
	Four
	Five
	Six
	Seven
	Eight
	Nine
	Ten
	Jack
	Queen
	King
	Ace
)

func (r Rank) Short() string {
	const chars = "23456789TJQKA"
	if int(r) >= len(chars) {
		return "?"
	}
	return string(chars[r])
}

func (r Rank) Name() string {
	switch r {
	case Two:
		return "Two"
	case Three:
		return "Three"
	case Four:
		return "Four"
	case Five:
		return "Five"
	case Six:
		return "Six"
	case Seven:
		return "Seven"
	case Eight:
		return "Eight"
	case Nine:
		return "Nine"
	case Ten:
		return "Ten"
	case Jack:
		return "Jack"
	case Queen:
		return "Queen"
	case King:
		return "King"
	case Ace:
		return "Ace"
	}
	return "?"
}

// Plural is the rank's plural form, used in hand descriptions ("Aces", "Sixes").
func (r Rank) Plural() string {
	switch r {
	case Six:
		return "Sixes"
	default:
		return r.Name() + "s"
	}
}

type Card struct {
	Suit Suit
	Rank Rank
}

// Short returns a 2-character code like "AS" or "TC".
func (c Card) Short() string {
	return c.Rank.Short() + c.Suit.String()
}

// Pretty returns e.g. "A\u2660".
func (c Card) Pretty() string {
	return c.Rank.Short() + c.Suit.Symbol()
}

func (c Card) String() string {
	return c.Short()
}

// Int encodes the card as its deck index in [0,52).
func (c Card) Int() int {
	return int(c.Suit)*13 + int(c.Rank)
}

// FromInt decodes a deck index in [0,52).
func FromInt(i int) (Card, error) {
	if i < 0 || i >= 52 {
		return Card{}, fmt.Errorf("card: int %d out of range [0,52)", i)
	}
	return Card{Suit: Suit(i / 13), Rank: Rank(i % 13)}, nil
}

// ParseDeckString decodes a 1-based wire string ("1".."52"). 1-based so
// SRA never sees the zero element.
func ParseDeckString(s string) (Card, error) {
	i, err := strconv.Atoi(s)
	if err != nil {
		return Card{}, fmt.Errorf("card: parse %q: %w", s, err)
	}
	return FromInt(i - 1)
}

// DeckString is the inverse of ParseDeckString.
func (c Card) DeckString() string {
	return strconv.Itoa(c.Int() + 1)
}

// ParseDeckStrings decodes a slice of wire-format card strings.
func ParseDeckStrings(ss []string) ([]Card, error) {
	out := make([]Card, len(ss))
	for i, s := range ss {
		c, err := ParseDeckString(s)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}
