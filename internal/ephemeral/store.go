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
//	"proposal-{roundID}-{attempt}"             - hosted by the proposer
//	"{roundID}-{attempt}_verify_{nodeID}"      - hosted by each verifier
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

// ProposalKey returns the canonical key for a round attempt's proposal file.
func ProposalKey(roundID uint64, attempt uint32) string {
	return fmt.Sprintf("proposal-%d-%d", roundID, attempt)
}

// VerifyKey returns the canonical key for a node's verify receipt in a round attempt.
func VerifyKey(roundID uint64, attempt uint32, nodeID string) string {
	return fmt.Sprintf("%d-%d_verify_%s", roundID, attempt, nodeID)
}

// AutoFoldKey returns the canonical key for an attestor's auto-fold signature
// hosted after K consecutive aborted attempts on roundID.
func AutoFoldKey(roundID uint64, signerID string) string {
	return fmt.Sprintf("auto_fold-%d-%s", roundID, signerID)
}

// CommunityRelayKey returns the canonical key for one seat's intermediate
// SRA decrypt of a community card. Each seat in the ring hosts at its own
// (cardIdx, seat) before the next seat polls and strips its layer.
func CommunityRelayKey(cardIdx, seat int) string {
	return fmt.Sprintf("community-relay-%d-%d", cardIdx, seat)
}

// CommunityCardKey returns the canonical key for the fully-decrypted
// plaintext of a community card. Hosted by the last seat in the ring;
// every peer polls it.
func CommunityCardKey(cardIdx int) string {
	return fmt.Sprintf("community-card-%d", cardIdx)
}

// AutoFoldPrefix returns the prefix shared by all auto-fold attestations for a
// round, used for bulk cleanup on a later commit (same lagging pattern as
// RoundPrefix does for verify receipts).
func AutoFoldPrefix(roundID uint64) string {
	return fmt.Sprintf("auto_fold-%d-", roundID)
}

// RoundPrefix returns the prefix shared by all ephemeral files for a round.
func RoundPrefix(roundID uint64) string {
	return fmt.Sprintf("%d-", roundID)
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
