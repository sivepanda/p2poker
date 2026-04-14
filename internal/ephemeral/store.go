package ephemeral

import (
	"fmt"
	"strings"
	"sync"
)

// Store is a thread-safe ephemeral key-value store.
// Each node hosts one locally; remote nodes poll for keys over the network.
// Values are opaque []byte to support arbitrary signed/hashed log data.
//
// Key convention for rounds:
//
//	"proposal-{roundID}"             - hosted by the proposer
//	"{roundID}_verify_{nodeID}"      - hosted by each verifier
type Store struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// New returns an initialized empty store.
func New() *Store {
	return &Store{
		data: make(map[string][]byte),
	}
}

// ProposalKey returns the canonical key for a round's proposal file.
func ProposalKey(roundID uint64) string {
	return fmt.Sprintf("proposal-%d", roundID)
}

// VerifyKey returns the canonical key for a node's verify receipt in a round.
func VerifyKey(roundID uint64, nodeID string) string {
	return fmt.Sprintf("%d_verify_%s", roundID, nodeID)
}

// RoundPrefix returns the prefix shared by all ephemeral files for a round.
func RoundPrefix(roundID uint64) string {
	return fmt.Sprintf("%d_", roundID)
}

// Put writes a key-value pair. Value is arbitrary bytes (signed log data, etc.).
func (s *Store) Put(key string, value []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

// Get retrieves a value by key. Returns (nil, false) if not found.
func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok
}

// Delete removes a key from the store.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
}

// DeletePrefix removes all keys with the given prefix.
// Used to clean up all ephemeral files for a completed round.
func (s *Store) DeletePrefix(prefix string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			delete(s.data, k)
		}
	}
}

// Keys returns all current keys.
func (s *Store) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys
}
