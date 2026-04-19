package game

import "fmt"

// State is the derived game state at a point in the log.
// Computed by replaying entries: never stored or transmitted.
type State struct {
	Stacks        []uint64 // remaining chips per seat
	Pot           uint64
	CurrentBet    uint64   // highest bet on the current street
	Contributions []uint64 // how much each player has put in this street
	Folded        []bool
	NumPlayers    uint8
}

// Replay walks the log entries and returns the derived state.
func Replay(entries []Entry, numPlayers uint8, startingStack uint64) State {
	s := State{
		Stacks:        make([]uint64, numPlayers),
		Contributions: make([]uint64, numPlayers),
		Folded:        make([]bool, numPlayers),
		NumPlayers:    numPlayers,
	}
	for i := range s.Stacks {
		s.Stacks[i] = startingStack
	}

	for _, e := range entries {
		s.apply(e)
	}
	return s
}

func (s *State) apply(e Entry) {
	p := e.PlayerID
	switch e.Action.Kind {
	case ActionFold:
		s.Folded[p] = true

	case ActionCheck:
		// no chip movement

	case ActionCall:
		cost := min(s.CurrentBet-s.Contributions[p], s.Stacks[p])
		s.Stacks[p] -= cost
		s.Contributions[p] += cost
		s.Pot += cost

	case ActionRaise:
		// Amount is the new total bet level (raise-to).
		raiseTo := e.Action.Amount
		cost := min(raiseTo-s.Contributions[p], s.Stacks[p])
		s.Stacks[p] -= cost
		s.Contributions[p] += cost
		s.Pot += cost
		s.CurrentBet = s.Contributions[p]
	}
}

// ValidateAction checks whether the proposed action is legal given current state.
func (s *State) ValidateAction(playerID uint8, action Action) error {
	if int(playerID) >= len(s.Stacks) {
		return fmt.Errorf("player %d out of range", playerID)
	}
	if s.Folded[playerID] {
		return fmt.Errorf("player %d already folded", playerID)
	}

	switch action.Kind {
	case ActionFold:
		if action.Amount != 0 {
			return fmt.Errorf("fold must have amount 0")
		}

	case ActionCheck:
		if action.Amount != 0 {
			return fmt.Errorf("check must have amount 0")
		}
		if s.Contributions[playerID] < s.CurrentBet {
			return fmt.Errorf("cannot check: must call or raise (owe %d)",
				s.CurrentBet-s.Contributions[playerID])
		}

	case ActionCall:
		if action.Amount != 0 {
			return fmt.Errorf("call must have amount 0 (cost is computed)")
		}
		owed := s.CurrentBet - s.Contributions[playerID]
		if owed == 0 {
			return fmt.Errorf("nothing to call, use check")
		}

	case ActionRaise:
		raiseTo := action.Amount
		if raiseTo <= s.CurrentBet {
			return fmt.Errorf("raise-to %d must exceed current bet %d", raiseTo, s.CurrentBet)
		}
		cost := raiseTo - s.Contributions[playerID]
		if cost > s.Stacks[playerID] {
			return fmt.Errorf("raise costs %d but player only has %d", cost, s.Stacks[playerID])
		}

	default:
		return fmt.Errorf("unknown action kind %d", action.Kind)
	}
	return nil
}
