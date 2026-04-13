package game

import "crypto/ed25519"

// Entry is a committed log entry. Doubles as a proposal before commit.
type Entry struct {
	RoundID   uint64
	PlayerID  uint8
	Action    Action
	Signature []byte
}

// Bytes: [8 RoundID BE][1 PlayerID][Action bytes][2 sig len BE][sig].
// Prior-entry sigs are hashed into the log chain
func (e Entry) Bytes() []byte {
	panic("not implemented")
}

type Log struct {
	entries []Entry
}

func NewLog() *Log {
	panic("not implemented")
}

// RoundID == len(entries); the round the next proposer signs under.
func (l *Log) RoundID() uint64 {
	panic("not implemented")
}

func (l *Log) Hash() []byte {
	panic("not implemented")
}

// Bytes returns the bytes Hash() hashes over.
func (l *Log) Bytes() []byte {
	panic("not implemented")
}

// ExpectedNextPlayer: deterministic function of the log (seat order from
// entry 0, folds, dealer button, street). Stub until rules engine exists.
func (l *Log) ExpectedNextPlayer() uint8 {
	panic("not implemented")
}

// Append adds e without verification. Caller must have verified first.
func (l *Log) Append(e Entry) {
	panic("not implemented")
}

// RollbackLast drops the last entry. Used on round abort.
func (l *Log) RollbackLast() {
	panic("not implemented")
}

// BuildProposal signs (l.Bytes() || action.Bytes()).
func (l *Log) BuildProposal(playerID uint8, action Action, sk ed25519.PrivateKey) Entry {
	panic("not implemented")
}

// VerifyProposal checks, in order:
//  1. p.RoundID == l.RoundID()
//  2. p.PlayerID == l.ExpectedNextPlayer()
//  3. p.Action legal given game state (TODO: rules)
//  4. sig over (l.Bytes() || p.Action.Bytes()) under pk
func (l *Log) VerifyProposal(p Entry, pk ed25519.PublicKey) error {
	panic("not implemented")
}

// VerifyReceipt is the ephemeral "I appended" file. Published AFTER Append.
type VerifyReceipt struct {
	RoundID     uint64
	PlayerID    uint8
	PostLogHash []byte // sha256 of log after applying the proposal
	Signature   []byte // sign(PostLogHash || "verify" || RoundID)
}

// BuildVerifyReceipt: call after Append(p) succeeds.
func (l *Log) BuildVerifyReceipt(playerID uint8, sk ed25519.PrivateKey) VerifyReceipt {
	panic("not implemented")
}

// VerifyVerifyReceipt checks RoundID, PostLogHash matches our log, and sig.
func (l *Log) VerifyVerifyReceipt(r VerifyReceipt, pk ed25519.PublicKey) error {
	panic("not implemented")
}
