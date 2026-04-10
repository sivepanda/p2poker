package deck

import (
	"crypto/rand"
	"errors"
	"math/big"
)

type Key struct {
	Exponent        *big.Int
	InverseExponent *big.Int
}

// Shuffle randomizes the order of cards in place.
func Shuffle(deck [][]byte) error {
	n := len(deck)
	for i := n - 1; i > 0; i-- {
		jBig, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}

		j := int(jBig.Int64())
		deck[i], deck[j] = deck[j], deck[i]
	}

	return nil
}

// EncryptDeck raises every card to the exponent under the prime.
func EncryptDeck(deck [][]byte, key *Key, prime *big.Int) [][]byte {
	encrypted := make([][]byte, len(deck))
	for i, card := range deck {
		m := new(big.Int).SetBytes(card)
		c := new(big.Int).Exp(m, key.Exponent, prime)
		encrypted[i] = c.Bytes()
	}

	return encrypted
}

// DecryptCard recovers the original card via the inverse exponent.
func DecryptCard(encryptedCard []byte, key *Key, prime *big.Int) []byte {
	c := new(big.Int).SetBytes(encryptedCard)
	m := new(big.Int).Exp(c, key.InverseExponent, prime)
	return m.Bytes()
}

// GenerateKey picks an exponent and its inverse modulo prime-1.
func GenerateKey(prime *big.Int) (*Key, error) {
	if prime == nil || prime.Sign() <= 0 {
		return nil, errors.New("prime must be set")
	}

	one := big.NewInt(1)
	phi := new(big.Int).Sub(prime, one)

	for {
		k, err := rand.Int(rand.Reader, phi)
		if err != nil {
			return nil, err
		}

		if k.Cmp(one) <= 0 {
			continue
		}

		gcd := new(big.Int)
		gcd.GCD(nil, nil, k, phi)
		if gcd.Cmp(one) != 0 {
			continue
		}

		kInv := new(big.Int).ModInverse(k, phi)
		if kInv == nil {
			continue
		}

		return &Key{Exponent: k, InverseExponent: kInv}, nil
	}
}
