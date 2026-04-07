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

func GenerateSigner() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func SignAction(privateKey ed25519.PrivateKey, action string) []byte {
	hash := sha256.Sum256([]byte(action))
	return ed25519.Sign(privateKey, hash[:])
}

func VerifyAction(publicKey ed25519.PublicKey, action string, signature []byte) bool {
	hash := sha256.Sum256([]byte(action))
	return ed25519.Verify(publicKey, hash[:], signature)
}

func NewEntry(authorID int, action string, privateKey ed25519.PrivateKey) Entry {
	return Entry{
		AuthorID:  authorID,
		Action:    action,
		Signature: SignAction(privateKey, action),
	}
}

func (e Entry) Verify(publicKey ed25519.PublicKey) bool {
	return VerifyAction(publicKey, e.Action, e.Signature)
}
