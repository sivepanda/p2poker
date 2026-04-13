package game

type ActionKind uint8

const (
	ActionFold ActionKind = iota
	ActionCheck
	ActionCall
	ActionRaise
)

type Action struct {
	Kind   ActionKind
	Amount uint64
}

// Bytes [1 byte Kind][8 bytes Amount BE]. Amount==0 unless Raise.
func (a Action) Bytes() []byte {
	panic("not implemented")
}

func UnmarshalAction(b []byte) (Action, error) {
	panic("not implemented")
}
