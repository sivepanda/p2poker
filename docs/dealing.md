# Card Dealing

This section describes the protocol by which a shared deck of cards is shuffled, dealt, and later verified in a peer-to-peer setting without any trusted third party. The core challenge is that no single player can be allowed to control the shuffle, no player should see another's cards during play, and yet every claim made at showdown must be provably true or false.

The algorithm achieves this through layered commutative encryption, a ring-based decryption protocol, and a commitment broadcast that binds each player's hand before they can see it.

## Table of Contents

- [Setup](#setup)
- [Phase 1: Shuffle](#phase-1-shuffle)
- [Phase 2: Deal](#phase-2-deal)
- [Phase 3: Gameplay](#phase-3-gameplay)
- [Phase 4: Showdown & Verification](#phase-4-showdown--verification)
- [Full Flow Summary](#full-flow-summary)
- [Problems & How This Algorithm Solves Them](#problems--how-this-algorithm-solves-them)

---

## Setup

Players establish a fixed circular order `A -> B -> C -> A` at game creation time. Each player generates a fresh ephemeral keypair for every game, bound to their long-term identity:

```
broadcast(sign("I am A, my ephemeral public key for this game is pk_A", long_term_key_A))
```

The ephemeral keypair is used for all cryptographic operations during the game — card encryption, action signing (see [consistency protocol](consistency.md)), and showdown verification. Generating a new keypair per game provides forward secrecy: compromising a key from one game reveals nothing about any other game. Long-term identity keys are used only to sign the ephemeral key at session start, never to encrypt card data.

---

## Phase 1: Shuffle

The shuffle phase ensures that no single player controls the final ordering of the deck. Each player shuffles and encrypts the deck in turn, so the result is the composition of all players' permutations under all players' encryption — and no one knows the final order.

The protocol requires a **commutative encryption scheme** (e.g., SRA) so that encryption layers can be removed in any order during the deal phase. Commutativity is what makes the ring-based decryption possible: it does not matter who decrypts first.

### Procedure

1. Player A begins with a plaintext array of 52 cards.
2. A shuffles the array, then encrypts every card individually with their private key. The result is `[A-encrypted]`. A forwards this to B.
3. B shuffles the array (now in a different order than A's shuffle, under A's encryption), then encrypts every card with their own private key. The result is `[AB-encrypted]`. B forwards this to C.
4. C shuffles and encrypts. The result is `[ABC-encrypted]`.
5. C broadcasts the fully-encrypted deck to all players. Every player stores this as the canonical deck.

At this point, all players hold an identical shuffled array of 52 cards under three layers of encryption. No player knows the final plaintext ordering. A knows only the permutation they applied before B and C shuffled again. The deck is the product of three independent shuffles, and its ordering is unknown to all.

---

## Phase 2: Deal

In poker, each player is dealt two private cards from the shared deck. Card indices are pre-assigned by seat position:

- A receives cards at indices `0, 1`
- B receives cards at indices `2, 3`
- C receives cards at indices `4, 5`

To deal cards to player X, every *other* player must remove their encryption layer from X's assigned cards. The cards travel around the ring, with each node peeling one layer, until only X's own encryption remains — which only X can remove.

### Procedure (dealing to Player A)

```
deck[0], deck[1] are [ABC-encrypted]

A sends deck[0], deck[1] to B
B decrypts with B's key:    [ABC] -> [AC-encrypted]     B forwards to C
C decrypts with C's key:    [AC]  -> [A-encrypted]      C holds result

// Commitment step: C broadcasts [A-encrypted] cards to ALL peers
// All peers store this as A's commitment
// C then forwards [A-encrypted] to A

A decrypts with A's key:    [A]   -> plaintext cards     A sees their hand
```

### The Commitment Broadcast

The critical step is the commitment broadcast. Before A ever sees their cards, the last decrypting node (C) publishes `[A-encrypted]` to the entire network. All players store this value. This creates a binding commitment: A's cards are locked in before A knows what they are.

This commitment is what makes showdown verification possible. Without it, A could claim any cards they wanted. With it, A's hand is fixed at deal time — any claim at showdown can be checked against the commitment by decrypting it with A's published key.

The same procedure runs for every player. After all rounds of dealing, every player holds:

- Their own plaintext cards (visible only to them)
- Every other player's `[X-encrypted]` commitment (verifiable at showdown)

---

## Phase 3: Gameplay

Players hold their cards privately. The game proceeds through betting rounds — raises, folds, checks, and calls — governed by the [consistency protocol](consistency.md). Card state is not involved in this phase; the dealing protocol has no further role until showdown.

---

## Phase 4: Showdown & Verification

When a player claims to have won, their cards must be verified against the commitment stored at deal time.

### Procedure (verifying Player A)

1. A publishes their ephemeral private key.
2. Any player retrieves the stored `[A-encrypted]` commitment from the deal phase.
3. They decrypt the commitment using A's published key: `decrypt([A-encrypted], A.private_key) -> plaintext cards`.
4. If the plaintext matches A's claimed hand, the claim is verified. If not, fraud is provable — A lied about their cards.

Because the commitment was broadcast before A saw their hand, and because only A's key can decrypt it, the verification is airtight. A cannot retroactively change their cards, and no other player could have tampered with the commitment without invalidating A's encryption layer.

---

## Full Flow Summary

```
SETUP         Each player generates ephemeral keypair
              Signs ephemeral public key with long-term identity key
              All peers exchange and store public keys

SHUFFLE       A shuffles + encrypts deck with A's key       -> [A-encrypted]
              B shuffles + encrypts with B's key             -> [AB-encrypted]
              C shuffles + encrypts with C's key             -> [ABC-encrypted]
              C broadcasts [ABC-encrypted] deck to all peers

DEAL          For each player X with assigned indices [i, j]:
                Cards deck[i], deck[j] travel the ring
                Each non-X player decrypts their layer
                Last decryptor broadcasts [X-encrypted] to all peers (commitment)
                All peers ACK the commitment
                X receives [X-encrypted], decrypts to plaintext

PLAY          Game proceeds under consistency protocol
              Cards remain private

SHOWDOWN      Claimant publishes ephemeral private key
              Any peer decrypts stored [claimant-encrypted] commitment
              Compares result to claimed hand
              Match -> verified       Mismatch -> provable fraud
```

---

## Problems & How This Algorithm Solves Them

---

### Problem 1: A single shuffler can control the deck order.

In centralized poker, a server shuffles the deck. Players must trust that the server is not rigging the order. In a peer-to-peer setting, no such trusted party exists — any single shuffler could arrange the deck to their advantage.

**Solution:** Every player shuffles and encrypts the deck in sequence. The final ordering is the composition of all players' independent permutations, applied under cumulative encryption so that no player can observe or reverse any other player's shuffle. No single player controls the result. Even if player A arranges the deck perfectly for themselves, B's shuffle (applied to A's encrypted output, with no visibility into the plaintext) destroys that arrangement, and C's shuffle destroys B's. The deck's final order is unknown to all participants.

---

### Problem 2: A player could claim different cards at showdown than they actually held.

If only the player sees their own cards, what prevents them from lying about their hand at showdown?

**Solution:** The commitment broadcast. Before a player ever sees their cards, the last decrypting node publishes the one-layer-encrypted version — `[X-encrypted]` — to the entire network. This commitment is stored by all peers before the player can decrypt it. At showdown, the player's published private key must decrypt the stored commitment to exactly their claimed cards. The commitment is made before the player knows their hand, so retroactive lying is impossible. The binding is cryptographic: changing the claim requires either forging a different commitment (which would require all other players' keys) or producing a different private key that decrypts the same ciphertext to different plaintext (which is computationally infeasible).

---

### Problem 3: A compromised key could expose past games.

If a player's private key is stolen or leaked, an attacker could decrypt recordings of old games and reveal that player's cards retroactively.

**Solution:** Players generate a fresh ephemeral keypair for every game. The key used to encrypt cards in one game is unrelated to the key used in any other. Compromising a single game's key reveals only that game's cards. Long-term identity keys are used only to sign the ephemeral public key at session start — they never touch card data, so compromising them reveals nothing about any game's cards.

---

### Problem 4: A player could go offline mid-deal to block the game.

If a node in the decryption ring refuses to lift their encryption layer, the deal stalls indefinitely. A single unresponsive player could hold the entire game hostage.

**Solution:** Each decryption step must complete within a defined timeout window. If a node fails to respond in time, the network flags a liveness fault. Because the commitment broadcast ensures all honest players have seen every intermediate state, the protocol can identify exactly which node stalled. The game can apply a penalty (forfeit the hand, eject the player) and proceed. Liveness faults are attributable because the ring structure makes the stalling node unambiguous — it is always the next expected decryptor.

---

### Problem 5: A player could intercept another player's cards during the decryption ring.

As cards travel the ring, intermediate nodes see partially-decrypted cards. Could they learn anything about another player's hand?

**Solution:** At every point in the ring, the cards retain at least one encryption layer that the intermediate node cannot remove — the destination player's layer. When B is decrypting A's cards, B sees `[AC-encrypted]` become `[A-encrypted]` — but B cannot remove A's layer, so the plaintext is never exposed to B. The commutative property of the encryption scheme ensures that layers can be removed in any order, but no node can remove a layer for which it does not hold the private key. Cards are only ever fully decrypted by their intended recipient.
