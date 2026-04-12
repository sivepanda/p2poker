package log

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
)

func GenerateSigner() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// Sign returns ed25519 sig over sha256(payload). Callers build payload (see game.Log.Bytes, game.Action.Bytes).
func Sign(sk ed25519.PrivateKey, payload []byte) []byte {
	h := sha256.Sum256(payload)
	return ed25519.Sign(sk, h[:])
}

func Verify(pk ed25519.PublicKey, payload, sig []byte) bool {
	h := sha256.Sum256(payload)
	return ed25519.Verify(pk, h[:], sig)
}
