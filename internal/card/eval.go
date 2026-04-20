package card

import (
	"fmt"
	"sort"
)

type Category uint8

const (
	HighCard Category = iota
	OnePair
	TwoPair
	ThreeOfAKind
	Straight
	Flush
	FullHouse
	FourOfAKind
	StraightFlush
)

func (c Category) String() string {
	switch c {
	case HighCard:
		return "High Card"
	case OnePair:
		return "One Pair"
	case TwoPair:
		return "Two Pair"
	case ThreeOfAKind:
		return "Three of a Kind"
	case Straight:
		return "Straight"
	case Flush:
		return "Flush"
	case FullHouse:
		return "Full House"
	case FourOfAKind:
		return "Four of a Kind"
	case StraightFlush:
		return "Straight Flush"
	}
	return "?"
}

// HandRank is the strength of a 5-card hand. Larger is stronger; compare
// with Compare. Best holds the 5 selected cards in display order
// (primary group first, then secondary, etc.).
type HandRank struct {
	Category    Category
	Tiebreakers [5]Rank
	Best        [5]Card
}

// Compare returns >0 if a is stronger than b, <0 if weaker, 0 if equal.
func Compare(a, b HandRank) int {
	if a.Category != b.Category {
		if a.Category > b.Category {
			return 1
		}
		return -1
	}
	for i := range 5 {
		if a.Tiebreakers[i] != b.Tiebreakers[i] {
			if a.Tiebreakers[i] > b.Tiebreakers[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

// Describe returns a human-readable summary like "Full House, Aces full of Kings".
func (h HandRank) Describe() string {
	t := h.Tiebreakers
	switch h.Category {
	case StraightFlush:
		if t[0] == Ace {
			return "Royal Flush"
		}
		return fmt.Sprintf("Straight Flush, %s-high", t[0].Name())
	case FourOfAKind:
		return fmt.Sprintf("Four of a Kind, %s", t[0].Plural())
	case FullHouse:
		return fmt.Sprintf("Full House, %s full of %s", t[0].Plural(), t[1].Plural())
	case Flush:
		return fmt.Sprintf("Flush, %s-high", t[0].Name())
	case Straight:
		return fmt.Sprintf("Straight, %s-high", t[0].Name())
	case ThreeOfAKind:
		return fmt.Sprintf("Three of a Kind, %s", t[0].Plural())
	case TwoPair:
		return fmt.Sprintf("Two Pair, %s and %s", t[0].Plural(), t[1].Plural())
	case OnePair:
		return fmt.Sprintf("Pair of %s", t[0].Plural())
	case HighCard:
		return fmt.Sprintf("High Card, %s", t[0].Name())
	}
	return h.Category.String()
}

// Best returns the strongest 5-card hand achievable from cards.
// Accepts 5, 6, or 7 cards (Texas Hold'em uses 7).
func Best(cards []Card) (HandRank, error) {
	switch {
	case len(cards) < 5:
		return HandRank{}, fmt.Errorf("card: Best needs at least 5 cards, got %d", len(cards))
	case len(cards) > 7:
		return HandRank{}, fmt.Errorf("card: Best supports at most 7 cards, got %d", len(cards))
	}
	if dup := firstDuplicate(cards); dup != nil {
		return HandRank{}, fmt.Errorf("card: duplicate card %s", dup)
	}

	var best HandRank
	first := true
	var combo [5]Card
	n := len(cards)
	// Walk all C(n,5) 5-subsets via two excluded indices for n=7, etc.
	// Enumerate masks with exactly 5 bits set.
	for mask := 1; mask < (1 << n); mask++ {
		if popcount(mask) != 5 {
			continue
		}
		idx := 0
		for k := range n {
			if mask&(1<<k) != 0 {
				combo[idx] = cards[k]
				idx++
			}
		}
		r := Rank5(combo)
		if first || Compare(r, best) > 0 {
			best = r
			first = false
		}
	}
	return best, nil
}

// Rank5 ranks a fixed 5-card hand.
func Rank5(hand [5]Card) HandRank {
	sorted := hand
	sort.Slice(sorted[:], func(i, j int) bool {
		return sorted[i].Rank > sorted[j].Rank
	})

	flush := sorted[0].Suit == sorted[1].Suit &&
		sorted[1].Suit == sorted[2].Suit &&
		sorted[2].Suit == sorted[3].Suit &&
		sorted[3].Suit == sorted[4].Suit

	ranks := [5]Rank{sorted[0].Rank, sorted[1].Rank, sorted[2].Rank, sorted[3].Rank, sorted[4].Rank}
	straight, straightHigh := detectStraight(ranks)

	if straight && flush {
		hr := HandRank{Category: StraightFlush, Best: arrangeStraight(sorted, straightHigh)}
		hr.Tiebreakers[0] = straightHigh
		return hr
	}

	groups := groupByRank(sorted)

	if groups[0].count == 4 {
		hr := HandRank{Category: FourOfAKind}
		hr.Tiebreakers[0] = groups[0].rank
		hr.Tiebreakers[1] = groups[1].rank
		hr.Best = arrangeByGroups(sorted, groups)
		return hr
	}

	if groups[0].count == 3 && len(groups) > 1 && groups[1].count == 2 {
		hr := HandRank{Category: FullHouse}
		hr.Tiebreakers[0] = groups[0].rank
		hr.Tiebreakers[1] = groups[1].rank
		hr.Best = arrangeByGroups(sorted, groups)
		return hr
	}

	if flush {
		hr := HandRank{Category: Flush, Best: sorted}
		for i := range 5 {
			hr.Tiebreakers[i] = sorted[i].Rank
		}
		return hr
	}

	if straight {
		hr := HandRank{Category: Straight, Best: arrangeStraight(sorted, straightHigh)}
		hr.Tiebreakers[0] = straightHigh
		return hr
	}

	if groups[0].count == 3 {
		hr := HandRank{Category: ThreeOfAKind}
		hr.Tiebreakers[0] = groups[0].rank
		hr.Tiebreakers[1] = groups[1].rank
		hr.Tiebreakers[2] = groups[2].rank
		hr.Best = arrangeByGroups(sorted, groups)
		return hr
	}

	if groups[0].count == 2 && len(groups) > 1 && groups[1].count == 2 {
		hr := HandRank{Category: TwoPair}
		hr.Tiebreakers[0] = groups[0].rank
		hr.Tiebreakers[1] = groups[1].rank
		hr.Tiebreakers[2] = groups[2].rank
		hr.Best = arrangeByGroups(sorted, groups)
		return hr
	}

	if groups[0].count == 2 {
		hr := HandRank{Category: OnePair}
		hr.Tiebreakers[0] = groups[0].rank
		hr.Tiebreakers[1] = groups[1].rank
		hr.Tiebreakers[2] = groups[2].rank
		hr.Tiebreakers[3] = groups[3].rank
		hr.Best = arrangeByGroups(sorted, groups)
		return hr
	}

	hr := HandRank{Category: HighCard, Best: sorted}
	for i := range 5 {
		hr.Tiebreakers[i] = sorted[i].Rank
	}
	return hr
}

type rankGroup struct {
	rank  Rank
	count int
}

func groupByRank(sorted [5]Card) []rankGroup {
	counts := make(map[Rank]int, 5)
	for _, c := range sorted {
		counts[c.Rank]++
	}
	groups := make([]rankGroup, 0, len(counts))
	for r, c := range counts {
		groups = append(groups, rankGroup{r, c})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].count != groups[j].count {
			return groups[i].count > groups[j].count
		}
		return groups[i].rank > groups[j].rank
	})
	return groups
}

// detectStraight returns (true, highRank) when ranks (sorted desc) form a
// straight. Wheel A-2-3-4-5 returns Five.
func detectStraight(r [5]Rank) (bool, Rank) {
	if r[0] == Ace && r[1] == Five && r[2] == Four && r[3] == Three && r[4] == Two {
		return true, Five
	}
	for i := range 4 {
		if r[i] != r[i+1]+1 {
			return false, 0
		}
	}
	return true, r[0]
}

func arrangeByGroups(sorted [5]Card, groups []rankGroup) [5]Card {
	var out [5]Card
	idx := 0
	for _, g := range groups {
		for _, c := range sorted {
			if c.Rank == g.rank {
				out[idx] = c
				idx++
			}
		}
	}
	return out
}

// arrangeStraight reorders a straight so the high card sits first. For the
// wheel (high=Five), the Ace drops to the back.
func arrangeStraight(sorted [5]Card, high Rank) [5]Card {
	if high != Five {
		return sorted
	}
	var out [5]Card
	idx := 0
	for _, c := range sorted {
		if c.Rank != Ace {
			out[idx] = c
			idx++
		}
	}
	out[4] = sorted[0]
	return out
}

func popcount(x int) int {
	n := 0
	for x != 0 {
		n += x & 1
		x >>= 1
	}
	return n
}

func firstDuplicate(cards []Card) *Card {
	seen := make(map[Card]struct{}, len(cards))
	for i := range cards {
		if _, ok := seen[cards[i]]; ok {
			return &cards[i]
		}
		seen[cards[i]] = struct{}{}
	}
	return nil
}
