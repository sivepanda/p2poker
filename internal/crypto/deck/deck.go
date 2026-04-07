package deck

import (
	"crypto/rand"
	"errors"
	"math/big"
)

type Key struct {
	K    *big.Int
	KInv *big.Int
}

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

func EncryptDeck(deck [][]byte, key *Key, prime *big.Int) [][]byte {
	encrypted := make([][]byte, len(deck))
	for i, card := range deck {
		m := new(big.Int).SetBytes(card)
		c := new(big.Int).Exp(m, key.K, prime)
		encrypted[i] = c.Bytes()
	}

	return encrypted
}

func DecryptCard(encryptedCard []byte, key *Key, prime *big.Int) []byte {
	c := new(big.Int).SetBytes(encryptedCard)
	m := new(big.Int).Exp(c, key.KInv, prime)
	return m.Bytes()
}

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

		return &Key{K: k, KInv: kInv}, nil
	}
}
