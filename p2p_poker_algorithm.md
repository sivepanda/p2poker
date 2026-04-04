# P2P Poker: Trustless Card Dealing Algorithm

## The Algorithm in Detail

### Setup

- Players establish a fixed circular order: `A → B → C → A`
- Each player generates a **fresh ephemeral keypair** for every game, bound to their long-term identity:
  > *"I am A (signed with my long-term key), and my ephemeral public key for this game is `pk_A`"*
- This ensures forward secrecy — past games cannot be decrypted even if a long-term key is later compromised.

---

### Phase 1: Shuffle (Encrypting the Deck)

1. Player A starts with a plaintext array of 52 cards.
2. Each player in order (A → B → C) does the following:
   - Shuffles the array
   - Encrypts every card with their own ephemeral private key using a **commutative encryption scheme** (e.g. SRA), so that layers can be removed in any order
3. After C encrypts, the deck has 3 layers of encryption: `[ABC-encrypted]`
4. C broadcasts the fully-encrypted deck to all players. Everyone stores this as the canonical deck.

---

### Phase 2: Dealing

Card indices are pre-assigned by position in the order:
- A receives cards at index `0, 1`
- B receives cards at index `2, 3`
- C receives cards at index `4, 5`
- And so on.

To deal cards to Player X, the other players must each lift their encryption layer. The cards travel around the ring, with each node removing their layer, until only X's layer remains.

**Example — dealing cards `0, 1` to Player A:**

```
[ABC-encrypted]  →  B lifts  →  [AC-encrypted]  →  C lifts  →  [A-encrypted]
                                                                       ↑
                                                             COMMIT PUBLISHED HERE
                                                             (before sending to A)
```

The last node to decrypt (C in this case) **broadcasts `[A-encrypted]` to all peers** and waits for acknowledgement from the network before forwarding the packet to A.

A then decrypts with their own private key to reveal their cards.

---

### Phase 3: Gameplay

Players hold their cards privately. No one else can read them — only the player who holds the final encryption layer can decrypt.

---

### Phase 4: Showdown & Verification

When a player (say A) claims to have won with cards `x` and `y`:

1. A publishes their ephemeral private key.
2. Any player can take the committed `[A-encrypted]` cards (stored before the game ended) and decrypt them using A's published key.
3. If the result matches A's claimed cards, the hand is verified. If not, fraud is provable.

---

### Full Flow Summary

```
SETUP       Each player generates ephemeral keypair, signs with long-term key

SHUFFLE     A shuffles + encrypts → B shuffles + encrypts → C shuffles + encrypts
            C broadcasts [ABC-encrypted] deck to all

DEAL        For each player X:
              Other nodes peel their layers one by one
              Last node broadcasts [X-encrypted] cards to all peers
              Network ACKs the commitment
              Last node forwards to X
              X decrypts with private key → plaintext cards

PLAY        Game proceeds; cards remain private

SHOWDOWN    Winning player publishes ephemeral private key
            Anyone verifies: decrypt([X-encrypted commitment]) == claimed cards
```

---

## Problems & How This Algorithm Solves Them

---

### Problem 1: Who shuffles the deck? A single shuffler can cheat.

In traditional online poker, a central server shuffles the deck. Players must trust that the server is honest and not rigging the order.

**Solution:** Every player shuffles and encrypts the deck before it is used. No single player controls the final order — the deck is the result of all shuffles combined. A cheating player can only influence the outcome if *all* other players also cheat.

---

### Problem 2: A player could claim different cards at showdown than they actually held.

Without a commitment, a player could wait to see the outcome, then lie about which cards they held to claim a win.

**Solution:** Before a player receives their decrypted cards, the last intermediary node broadcasts a commitment — the one-layer-encrypted version of those cards — to the entire network. This is published *before* the player can see their cards. At showdown, the player's revealed private key must decrypt the stored commitment to exactly their claimed cards. The commitment is made before the player knows their hand, so retroactive lying is impossible.

---

### Problem 3: The node publishing the commitment could lie about what it broadcasts.

The last decrypting node (e.g. C for Player A's cards) both creates the commitment and forwards the packet to A. What stops C from publishing a fake commitment and sending A different cards?

**Solution:** A can detect this immediately. After decrypting their cards, A re-encrypts with their own public key and checks whether the result matches what C published to the network. If C sent A a different packet than what was broadcast, the mismatch is cryptographically provable. A can reveal their private key to the network purely for dispute purposes, demonstrating the fraud without any reliance on trust.

---

### Problem 4: A compromised game session exposes all past games.

If a player's private key is stolen or leaked, an attacker could potentially decrypt recordings of old games.

**Solution:** Players generate a fresh ephemeral keypair for every game. Even if a key from one game is compromised, it reveals nothing about any other game. Long-term identity keys are only used to sign the ephemeral key at the start of a session, not to encrypt any card data.

---

### Problem 5: A player could go offline mid-deal to block the game.

If a node in the decryption chain (e.g. B) refuses to lift their encryption layer, the deal stalls indefinitely.

**Solution:** The protocol requires each decryption step to complete within a defined timeout window. If a node fails to respond in time, the network can flag a liveness fault and apply a penalty or abandon the round. The committed deck (broadcast at the end of the shuffle phase) also serves as evidence of who held up the game.

---

### Problem 6: A player could collude with others to peek at an opponent's cards.

In a 3-player game, if B and C cooperate, they both hold encryption layers over A's cards during the deal phase. Together they could reconstruct A's plaintext cards before A decrypts them.

**Solution:** This is a known fundamental limitation of mental poker with no trusted third party. Any N-1 players can collude against the Nth. The algorithm does not eliminate this risk but makes collusion *detectable after the fact* via the commitment and key publication steps. Reputation systems, stake-based deterrents, or larger player pools can reduce the practical risk.
