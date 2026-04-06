package main

import (
	"crypto/rand"
	"math/big"
)

// Key holds a keypair for commutative encryption.
// Because encryption is commutative (order doesn't matter),
// each node can encrypt/decrypt independently without coordinating key exchange.
type Key struct {
	K    *big.Int // encryption exponent
	KInv *big.Int // decryption exponent — modular inverse of K mod (P-1)
}

// DealingRequest circulates around the player ring during the deal phase.
// Each node partially decrypts value1/value2 as the request passes through.
// When the request returns to its origin, both values are fully decrypted.
type DealingRequest struct {
	id     int    // ID of the node that owns these cards
	index1 int    // index of card 1 in the final deck
	index2 int    // index of card 2 in the final deck
	value1 []byte // encrypted card value, replaced with decrypted value after full ring traversal
	value2 []byte // encrypted card value, replaced with decrypted value after full ring traversal
}

// --- SETUP ---

// SetupGrids initializes the communication channels between all nodes.
// ShuffleGrid[i][j] carries the deck from node i to node j during the shuffle phase.
// DealingGrid[i][j] carries deal requests from node i to node j during the deal phase.
// Channels are buffered by 1 to prevent deadlocks on sequential send/receive pairs.
func SetupGrids() {
	for i := range ShuffleGrid {
		ShuffleGrid[i] = make([]chan [][]byte, N)
		DealingGrid[i] = make([]chan DealingRequest, N)
		for j := range ShuffleGrid[i] {
			ShuffleGrid[i][j] = make(chan [][]byte, 1)
			DealingGrid[i][j] = make(chan DealingRequest, 1)
		}
	}
}

// --- DECK OPERATIONS ---

// ShuffleDeck performs a Fisher-Yates shuffle in place using a
// cryptographically secure random source to prevent prediction.
func ShuffleDeck(deck [][]byte) {
	n := len(deck)
	for i := n - 1; i > 0; i-- {
		jBig, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := int(jBig.Int64())
		deck[i], deck[j] = deck[j], deck[i]
	}
}

// EncryptDeck applies this node's encryption key to every card in the deck.
// Each card is treated as a big.Int and raised to the power K mod P.
// Returns a new deck — the original is not modified.
func EncryptDeck(deck [][]byte, key *Key) [][]byte {
	encrypted := make([][]byte, len(deck))
	for i, card := range deck {
		m := new(big.Int).SetBytes(card)
		// ElGamal-style encryption: c = m^K mod P
		c := new(big.Int).Exp(m, key.K, P)
		encrypted[i] = c.Bytes()
	}
	return encrypted
}

// DecryptCard applies this node's decryption key to a single encrypted card.
// Reverses EncryptDeck: m = c^KInv mod P
func DecryptCard(encryptedCard []byte, key *Key) []byte {
	c := new(big.Int).SetBytes(encryptedCard)
	// ElGamal-style decryption: m = c^KInv mod P
	m := new(big.Int).Exp(c, key.KInv, P)
	return m.Bytes()
}

// --- KEY GENERATION ---

// GenerateKey produces a random keypair valid for commutative encryption over P.
// Requires gcd(K, P-1) = 1 so that K has a modular inverse mod (P-1),
// guaranteeing that encryption is reversible.
func GenerateKey() *Key {
	one := big.NewInt(1)
	phi := new(big.Int).Sub(P, one) // Euler's totient: φ(P) = P-1 for prime P

	for {
		k, _ := rand.Int(rand.Reader, phi)
		if k.Cmp(one) <= 0 {
			continue // k must be > 1
		}

		gcd := new(big.Int)
		gcd.GCD(nil, nil, k, phi)
		if gcd.Cmp(one) != 0 {
			continue // k must be coprime with φ(P)
		}

		kInv := new(big.Int).ModInverse(k, phi)
		return &Key{K: k, KInv: kInv}
	}
}
