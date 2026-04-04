# Card Dealing

## The Algorithm in Detail

### Setup

- Players establish a fixed circular order: `A → B → C → A`
- Each player generates a **fresh ephemeral keypair** for every game, bound to their long-term identity:
  > *"I am A (signed with my long-term key), and my ephemeral public key for this game is `pk_A`"*
- This ensures forward secrecy — past games cannot be decrypted even if a long-term key is later compromised.
- Every game is played with a unique set of private keys for each player

---

### Phase 1: Shuffle (Encrypting the Deck)

1. Player A starts with a plaintext array of 52 cards.
2. Each player in order (A → B → C) does the following:
   - Shuffles the array
   - Encrypts every card with their own private key using a **commutative encryption scheme** (e.g. SRA), so that layers can be removed in any order
3. After C encrypts, the deck has 3 layers of encryption: `[ABC-encrypted]`
4. C broadcasts the fully-encrypted deck to all players. Everyone stores this as the canonical deck.
   - At this point, every played has a shuffled array of 52 cards, under three layes of encyrption

---

### Phase 2: Dealing

In Poker, each player starts off with two cards, drawn from a shared deck. Therefore, each player must be asigned two unique indices, which represent the index of their two cards in the deck/array.

Card indices are pre-assigned by position in the order:
- A receives cards at index `0, 1`
- B receives cards at index `2, 3`
- C receives cards at index `4, 5`
- And so on.

To deal cards to Player X, the other players must each lift their encryption layer. The cards travel around the ring, with each node removing their layer, until only X's layer remains.

**Example — dealing cards `0, 1` to Player A:**

```
//A begins a request to B to remove the encryption on deck[0] and deck[1]

[ABC-encrypted]  →  B lifts  →  [AC-encrypted]  →  C lifts  →  [A-encrypted] -> A (can decrypt)

When the cards get back to A, they are only [A-encrypted] meaning A can reveal their cards

//NOTE: No-one else ever saw A's unencrypted cards

//NOTE: A can never decrypt any other cards (say at index 9). A doesn't own that index
```

To make sure A can never lie about their cards in the future, an additional step is performed:

The last node to decrypt someone else's cards (C in this case) **broadcasts `[A-encrypted]` to all peers** and waits for acknowledgement from the network before forwarding the packet to A.

```
[ABC-encrypted]  →  B lifts  →  [AC-encrypted]  →  C lifts  →  [A-encrypted] -> A (can decrypt)
                                                                     ↑
                                                  [A-encrypted] BROADCASTED TO ALL BY C
```


`[A-encrypted]`, `[B-encrypted]`, and `[C-encrypted]` is public knowledge, stored by A, B, and C


---

### Phase 3: Gameplay

Players hold their cards privately. The game goes on, as players raise, fold, check, and call...

TODO: Develop the logic for logging all operations as they go around the ring.

---

### Phase 4: Showdown & Verification

When a player (say A) claims to have won with cards `x` and `y`:

1. A publishes their private key.
2. Any player can take their `[A-encrypted]` cards (stored before the game started) and decrypt them using A's published key.
3. If the result matches A's claimed cards, the hand is verified. If not, fraud is provable. A lied about their cards

---

### Full Flow Summary

```
SETUP       Each player generates ephemeral keypair, signs with long-term key

SHUFFLE     A shuffles + encrypts → B shuffles + encrypts → ... ->  Z shuffles + encrypts
            Z broadcasts [ABC..Z-encrypted] deck to all

DEAL        For each player, say Y:
              Start a request around the ring for everyone to decrypt array[index y1, index y2]
              Other nodes peel their layers one by one
              Last node [X] broadcasts [Y-encrypted] cards to all peers
              Network ACKs the commitment
              Y decrypts with private key → plaintext cards

PLAY        Game proceeds; cards remain private

SHOWDOWN    Winning [Y] player publishes private key along with their claimed cards
            Anyone verifies: decrypt([Y-encrypted]) == claimed cards
            Alternatively encypt(claimed cards) = [Y-encrypted]
```

---

## Problems & How This Algorithm Solves Them

---

### Problem 1: Who shuffles the deck? A single shuffler can cheat.

In traditional online poker, a central server shuffles the deck. Players must trust that the server is honest and not rigging the order.

In a p2p network, how do we guarantee that (a) the cards are shuffled (b) no one sees anyone else's cards (c) everyone still draws from a shared deck

**Solution:** Every player shuffles and encrypts the deck before it is used. No single player controls the final order — the deck is the result of all shuffles combined. No one can see anyone else's cards because they are always in an encrypted state until their reach their owner. No one shares the same cards because everyone draws from different parts of the deck.

---

### Problem 2: A player could claim different cards at showdown than they actually held.

If player A is the only one who saw their cards, couldnt they just lie?

**Solution:** Before a player receives their decrypted cards, the last intermediary node broadcasts a commitment — the one-layer-encrypted version of those cards — to the entire network. This is published *before* the player can see their cards. At showdown, the player's revealed private key must decrypt the stored commitment to exactly their claimed cards. The commitment is made before the player knows their hand, so retroactive lying is impossible.



---

### Problem 3: A compromised game session exposes all past games.

If a player's private key is stolen or leaked, an attacker could potentially decrypt recordings of old games.

**Solution:** Players generate a fresh ephemeral keypair for every game. Even if a key from one game is compromised, it reveals nothing about any other game. Long-term identity keys are only used to sign the ephemeral key at the start of a session, not to encrypt any card data.

---

### Problem 5: A player could go offline mid-deal to block the game.

If a node in the decryption chain (e.g. B) refuses to lift their encryption layer, the deal stalls indefinitely.

**Solution:** The protocol requires each decryption step to complete within a defined timeout window. If a node fails to respond in time, the network can flag a liveness fault and apply a penalty or abandon the round. All actions are broadcasted, but not all broadcasts need an ACK to proceed to the next step. This way, it become possible for good note-takers to figure out who is stalling. 


### Conclusion

The system is set up so that everyone can securely get their cards (in just 2 round trips) and can verify the truth of all claims

Verification of truth is never a burden on one person. Everyone has enough information to verify. Truth is a property of the network.

