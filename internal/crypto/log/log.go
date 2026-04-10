package log

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
)

type Entry struct {
	AuthorID  int
	Action    string
	Signature []byte
}

// GenerateSigner returns a fresh ed25519 key pair.
func GenerateSigner() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// SignAction signs the action string with the private key.
func SignAction(privateKey ed25519.PrivateKey, action string) []byte {
	hash := sha256.Sum256([]byte(action))
	return ed25519.Sign(privateKey, hash[:])
}

// VerifyAction checks the ed25519 signature against the action.
func VerifyAction(publicKey ed25519.PublicKey, action string, signature []byte) bool {
	hash := sha256.Sum256([]byte(action))
	return ed25519.Verify(publicKey, hash[:], signature)
}

// NewEntry builds a log entry signed by the given author.
func NewEntry(authorID int, action string, privateKey ed25519.PrivateKey) Entry {
	return Entry{
		AuthorID:  authorID,
		Action:    action,
		Signature: SignAction(privateKey, action),
	}
}

// Verify confirms the entry's signature matches the public key.
func (e Entry) Verify(publicKey ed25519.PublicKey) bool {
	return VerifyAction(publicKey, e.Action, e.Signature)
}
