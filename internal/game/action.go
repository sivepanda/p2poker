package game

import (
	"encoding/binary"
	"errors"
)

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
	buf := make([]byte, 9)
	buf[0] = byte(a.Kind)
	binary.BigEndian.PutUint64(buf[1:], a.Amount)
	return buf
}

func UnmarshalAction(b []byte) (Action, error) {
	if len(b) < 9 {
		return Action{}, errors.New("action bytes too short")
	}
	return Action{
		Kind:   ActionKind(b[0]),
		Amount: binary.BigEndian.Uint64(b[1:]),
	}, nil
}
