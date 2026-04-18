package game

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	cryptolog "github.com/sivepanda/p2poker/internal/crypto/log"
)

// Entry is a committed log entry. Doubles as a proposal before commit.
type Entry struct {
	RoundID   uint64
	PlayerID  uint8
	Action    Action
	Signature []byte
	Data      []byte // arbitrary round-specific payload (e.g. node order in entry 0)
}

// Bytes [8 RoundID BE][1 PlayerID][Action bytes][2 sig len BE][sig][4 data len BE][data].
// Prior-entry sigs are hashed into the log chain
func (e Entry) Bytes() []byte {
	buf := make([]byte, 8+1)
	binary.BigEndian.PutUint64(buf[:8], e.RoundID)
	buf[8] = e.PlayerID
	buf = append(buf, e.Action.Bytes()...)

	sigLen := make([]byte, 2)
	binary.BigEndian.PutUint16(sigLen, uint16(len(e.Signature)))
	buf = append(buf, sigLen...)
	buf = append(buf, e.Signature...)

	dataLen := make([]byte, 4)
	binary.BigEndian.PutUint32(dataLen, uint32(len(e.Data)))
	buf = append(buf, dataLen...)
	buf = append(buf, e.Data...)

	return buf
}

type Log struct {
	entries       []Entry
	numPlayers    uint8
	startingStack uint64
}

func NewLog(numPlayers uint8, startingStack uint64) *Log {
	return &Log{
		numPlayers:    numPlayers,
		startingStack: startingStack,
	}
}

// RoundID == len(entries); the round the next proposer signs under.
func (l *Log) RoundID() uint64 {
	return uint64(len(l.entries))
}

// Bytes returns the bytes Hash() hashes over.
func (l *Log) Bytes() []byte {
	var buf []byte
	for _, e := range l.entries {
		buf = append(buf, e.Bytes()...)
	}
	return buf
}

func (l *Log) Hash() []byte {
	h := sha256.Sum256(l.Bytes())
	return h[:]
}

// ExpectedNextPlayer deterministic function of the log (seat order from
// entry 0, folds, dealer button, street). Streets are
// not yet modeled; folds are.
func (l *Log) ExpectedNextPlayer() uint8 {
	if l.numPlayers == 0 {
		return 0
	}
	// we "replay" the log to figure out who folded. logs are guaranteed consistent bc the nature of proposals
	state := Replay(l.entries, l.numPlayers, l.startingStack)
	n := uint64(l.numPlayers)
	start := l.RoundID() % n
	for i := range n {
		cand := uint8((start + i) % n)
		if !state.Folded[cand] {
			return cand
		}
	}
	return uint8(start)
}

// NumPlayers returns the configured player count.
func (l *Log) NumPlayers() uint8 {
	return l.numPlayers
}

// StartingStack returns the starting stack size used to replay state.
func (l *Log) StartingStack() uint64 {
	return l.startingStack
}

// Entries returns a copy of the log entries.
func (l *Log) Entries() []Entry {
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Append adds e without verification. Caller must have verified first.
func (l *Log) Append(e Entry) {
	l.entries = append(l.entries, e)
}

// RollbackLast drops the last entry. Used on round abort.
func (l *Log) RollbackLast() {
	if len(l.entries) > 0 {
		l.entries = l.entries[:len(l.entries)-1]
	}
}

// BuildProposal signs (l.Bytes() || action.Bytes()).
func (l *Log) BuildProposal(playerID uint8, action Action, sk ed25519.PrivateKey) Entry {
	payload := append(l.Bytes(), action.Bytes()...)
	sig := cryptolog.Sign(sk, payload)
	return Entry{
		RoundID:   l.RoundID(),
		PlayerID:  playerID,
		Action:    action,
		Signature: sig,
	}
}

// VerifyProposal checks, in order:
//  1. p.RoundID == l.RoundID()
//  2. p.PlayerID == l.ExpectedNextPlayer()
//  3. sig over (l.Bytes() || p.Action.Bytes()) under pk
//  4. p.Action legal given game state
func (l *Log) VerifyProposal(p Entry, pk ed25519.PublicKey) error {
	if p.RoundID != l.RoundID() {
		return errors.New("round id mismatch")
	}
	if p.PlayerID != l.ExpectedNextPlayer() {
		return errors.New("player id mismatch")
	}
	if len(pk) != ed25519.PublicKeySize {
		return errors.New("verifier has no public key for proposer")
	}
	if !cryptolog.Verify(pk, append(l.Bytes(), p.Action.Bytes()...), p.Signature) {
		return errors.New("invalid proposal signature")
	}

	state := Replay(l.entries, l.numPlayers, l.startingStack)
	if err := state.ValidateAction(p.PlayerID, p.Action); err != nil {
		return fmt.Errorf("illegal action: %w", err)
	}
	return nil
}

// VerifyReceipt is the ephemeral "I appended" file
type VerifyReceipt struct {
	RoundID     uint64
	PlayerID    uint8
	PostLogHash []byte // sha256 of log after applying the proposal
	Signature   []byte // sign(PostLogHash || "verify" || RoundID)
}

// BuildVerifyReceipt: call after Append(p) succeeds.
func (l *Log) BuildVerifyReceipt(playerID uint8, sk ed25519.PrivateKey) VerifyReceipt {
	hash := l.Hash()
	roundID := l.RoundID() - 1 // receipt is for the entry just appended

	var payload []byte
	payload = append(payload, hash...)
	payload = append(payload, []byte("verify")...)
	roundBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(roundBuf, roundID)
	payload = append(payload, roundBuf...)

	sig := cryptolog.Sign(sk, payload)
	return VerifyReceipt{
		RoundID:     roundID,
		PlayerID:    playerID,
		PostLogHash: hash,
		Signature:   sig,
	}
}

// VerifyVerifyReceipt checks RoundID, PostLogHash matches our log, and sig.
func (l *Log) VerifyVerifyReceipt(r VerifyReceipt, pk ed25519.PublicKey) error {
	// 1. Basic Round Check
	if r.RoundID != l.RoundID()-1 {
		return fmt.Errorf("receipt round mismatch: got %d, want %d", r.RoundID, l.RoundID()-1)
	}

	// 2. Hash Consistency Check (CRITICAL)
	if !bytes.Equal(r.PostLogHash, l.Hash()) {
		return fmt.Errorf("receipt hash does not match local log state")
	}

	// 3. Reconstruct the exact same payload used in Build
	payload := make([]byte, 0, len(r.PostLogHash)+14)
	payload = append(payload, r.PostLogHash...)
	payload = append(payload, "verify"...)
	roundBuf := [8]byte{}
	binary.BigEndian.PutUint64(roundBuf[:], r.RoundID)
	payload = append(payload, roundBuf[:]...)

	// 4. Crypto Verification
	if !cryptolog.Verify(pk, payload, r.Signature) {
		return fmt.Errorf("invalid signature from player %d", r.PlayerID)
	}

	return nil
}
