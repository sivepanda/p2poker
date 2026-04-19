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

// autoFoldTag is mixed into the attestation payload to domain-separate it from
// regular proposal signatures.
const autoFoldTag = "auto_fold"

// Entry is a committed log entry. Doubles as a proposal before commit.
//
// Auto-fold entries are distinguished by (Action.Kind == ActionFold &&
// len(Signature) == 0). In that case CoSigners/CoSignatures carry the
// ed25519 attestations that stand in for the absent self-signature.
type Entry struct {
	RoundID      uint64
	PlayerID     uint8 // for auto-fold, this is the target seat
	Action       Action
	Signature    []byte
	CoSigners    []uint8  // auto-fold only: attestor seats, sorted ascending
	CoSignatures [][]byte // auto-fold only: same length & order as CoSigners
	Data         []byte   // arbitrary round-specific payload (e.g. node order in entry 0)
}

// IsAutoFold reports whether e is an auto-fold entry (fold action, no
// self-signature, relying on CoSigners/CoSignatures instead).
func (e Entry) IsAutoFold() bool {
	return e.Action.Kind == ActionFold && len(e.Signature) == 0
}

// Bytes encoding:
//
//	[8 RoundID BE][1 PlayerID][Action(9)][2 sigLen BE][sig]
//	if auto-fold:
//	    [1 cosigCount]
//	    for each cosig: [1 signerID][2 cosigLen BE][cosig bytes]
//	[4 dataLen BE][data]
//
// Normal (signed) entries omit the cosig section entirely, preserving the
// hash-chain encoding used by prior tests and committed logs.
func (e Entry) Bytes() []byte {
	buf := make([]byte, 8+1)
	binary.BigEndian.PutUint64(buf[:8], e.RoundID)
	buf[8] = e.PlayerID
	buf = append(buf, e.Action.Bytes()...)

	sigLen := make([]byte, 2)
	binary.BigEndian.PutUint16(sigLen, uint16(len(e.Signature)))
	buf = append(buf, sigLen...)
	buf = append(buf, e.Signature...)

	if e.IsAutoFold() {
		buf = append(buf, byte(len(e.CoSigners)))
		for i, signer := range e.CoSigners {
			buf = append(buf, signer)
			sig := e.CoSignatures[i]
			cosigLen := make([]byte, 2)
			binary.BigEndian.PutUint16(cosigLen, uint16(len(sig)))
			buf = append(buf, cosigLen...)
			buf = append(buf, sig...)
		}
	}

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

// autoFoldPayload: logBytes || "auto_fold" || roundID(8BE) || targetSeat(1).
func (l *Log) autoFoldPayload(roundID uint64, targetSeat uint8) []byte {
	logBytes := l.Bytes()
	payload := make([]byte, 0, len(logBytes)+len(autoFoldTag)+9)
	payload = append(payload, logBytes...)
	payload = append(payload, autoFoldTag...)
	roundBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(roundBuf, roundID)
	payload = append(payload, roundBuf...)
	payload = append(payload, targetSeat)
	return payload
}

// BuildAutoFoldAttestation: one attestor's ed25519 sig over autoFoldPayload.
func (l *Log) BuildAutoFoldAttestation(roundID uint64, targetSeat uint8, sk ed25519.PrivateKey) []byte {
	return cryptolog.Sign(sk, l.autoFoldPayload(roundID, targetSeat))
}

// VerifyAutoFoldAttestation checks one attestor's sig.
func (l *Log) VerifyAutoFoldAttestation(roundID uint64, targetSeat uint8, pk ed25519.PublicKey, sig []byte) error {
	if len(pk) != ed25519.PublicKeySize {
		return errors.New("attestor has no public key")
	}
	if !cryptolog.Verify(pk, l.autoFoldPayload(roundID, targetSeat), sig) {
		return errors.New("invalid auto-fold attestation signature")
	}
	return nil
}

// ExpectedAutoFoldAttestors: non-folded seats minus target, sorted asc.
func (l *Log) ExpectedAutoFoldAttestors(targetSeat uint8) []uint8 {
	state := Replay(l.entries, l.numPlayers, l.startingStack)
	out := make([]uint8, 0, l.numPlayers)
	for seat := uint8(0); seat < l.numPlayers; seat++ {
		if seat == targetSeat || state.Folded[seat] {
			continue
		}
		out = append(out, seat)
	}
	return out
}

// VerifyAutoFoldEntry: round match, auto-fold shape, target valid/unfolded,
// CoSigners equals expected set, every co-sig verifies.
func (l *Log) VerifyAutoFoldEntry(e Entry, pks map[uint8]ed25519.PublicKey) error {
	if e.RoundID != l.RoundID() {
		return fmt.Errorf("auto-fold round mismatch: got %d, want %d", e.RoundID, l.RoundID())
	}
	if !e.IsAutoFold() {
		return errors.New("entry is not an auto-fold")
	}
	if e.PlayerID >= l.numPlayers {
		return fmt.Errorf("auto-fold target seat %d out of range", e.PlayerID)
	}
	state := Replay(l.entries, l.numPlayers, l.startingStack)
	if state.Folded[e.PlayerID] {
		return fmt.Errorf("auto-fold target seat %d already folded", e.PlayerID)
	}
	expected := l.ExpectedAutoFoldAttestors(e.PlayerID)
	if len(expected) == 0 {
		return errors.New("no eligible attestors for auto-fold")
	}
	if len(e.CoSigners) != len(expected) {
		return fmt.Errorf("auto-fold co-signer count %d, want %d", len(e.CoSigners), len(expected))
	}
	if len(e.CoSignatures) != len(e.CoSigners) {
		return fmt.Errorf("auto-fold co-signatures/co-signers length mismatch (%d vs %d)", len(e.CoSignatures), len(e.CoSigners))
	}
	for i, seat := range e.CoSigners {
		if seat != expected[i] {
			return fmt.Errorf("auto-fold co-signer[%d] = %d, want %d", i, seat, expected[i])
		}
		pk, ok := pks[seat]
		if !ok {
			return fmt.Errorf("auto-fold attestor %d: no public key registered", seat)
		}
		if err := l.VerifyAutoFoldAttestation(e.RoundID, e.PlayerID, pk, e.CoSignatures[i]); err != nil {
			return fmt.Errorf("auto-fold attestor %d: %w", seat, err)
		}
	}
	return nil
}

// AppendAutoFold verifies then appends. No mutation on failure.
func (l *Log) AppendAutoFold(e Entry, pks map[uint8]ed25519.PublicKey) error {
	if err := l.VerifyAutoFoldEntry(e, pks); err != nil {
		return err
	}
	l.entries = append(l.entries, e)
	return nil
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
