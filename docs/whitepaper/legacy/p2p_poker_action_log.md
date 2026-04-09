# P2P Poker: Trustless Action Log Algorithm

## The Algorithm in Detail

### Setup

Players use the same ephemeral keypairs established during the card dealing phase. The circular order `A → B → C → A` carries over. Each player's public key is already known to all peers, so signatures can be verified by anyone at any time.

---

### Structure of the Log

The log is an ordered list of entries. Each entry is a tuple:

```

( plaintext_action, encrypt(hash(plaintext_action), private_key) )

Equivalently:

( plaintext_action, sign(plaintext_action) )

//note the private key is not passed around, it is just used to encrypt a hash of the action

```

- **`plaintext_action`** — a human-readable action and its parameters, e.g. `{ action: "Raise", amount: 50 }`
- **`sign(hash(plaintext_action), private_key)`** — the acting player hashes their plaintext action, then signs that hash with their ephemeral private key

Actions are public by design — all players should eventually know what everyone did. The signatire verifies who wrote it.

---

### Phase: Taking a Turn

When it is a player's turn, they receive the current log from the previous player. They then do the following **in order**:

**Step 1 — Verify new entries**

For every log entry added since this player's last turn, in order:

```
expected_author  = who should have acted at this position in the log
//easy to calculate given the initial sequence A -> B -> C -> A

received_hash    = decrypt(signature, expected_author.public_key)
computed_hash    = hash(plaintext_action)

assert received_hash == computed_hash
```

If any entry fails verification, the player halts and raises a dispute with the full log as evidence.

Anyone could verify the log because it relies only on public keys and the rules of poker.

**Step 2 — Replay actions in the frontend**

For each verified new entry, the client plays out the action visually so the player is fully caught up before acting.

**Step 3 — Make an action**

The player selects their action (Check, Fold, Raise, Call, etc.). They then append a new entry to the log:

```
plaintext  = {  action: chosen_action, ...params }
signature  = sign(hash(plaintext), self.private_key)
log.append( plaintext, signature )

//This is the same structure discussed above
```

**Step 4 — Pass the log**

The player forwards the full log to the next player in the circle.

---

### Full Flow Summary

```
TURN START    Receive log from previous player

VERIFY        For each new entry (in order):
                - Hash the plaintext
                - Decrypt the signature using the expected author's public key
                - Assert they match

REPLAY        Play out each verified action in the frontend

ACT           Player selects action
              Append ( plaintext, sign(hash(plaintext), private_key) ) to log

PASS          Forward full log to next player
```

---

## Problems & How This Algorithm Solves Them

---

### Problem 1: A player could lie about what action they took.

A player might claim they folded when they actually raised, or deny having acted at all.

**Solution:** Every action is signed with the acting player's ephemeral private key. Since only that player holds their private key, a valid signature is proof that they and only they produced that entry. A player cannot disown a log entry that bears a valid signature from their own key — "I didn't make that action" is not a viable defence.

---

### Problem 2: A player could try to take back an action after seeing new information.

After acting, a player might want to retroactively change their entry — e.g. switch a Call to a Fold after seeing an opponent's behaviour.

**Solution:** The log is append-only and each entry is signed at the moment of action. Once a signed entry is in the log and the next player has received it, it cannot be altered without invalidating the signature. Any modification to the plaintext produces a different hash, which no longer matches the stored signature. The tamper is immediately detectable.

---

### Problem 3: A player could try to swap or reorder entries to change the game state.

A malicious player might try to reorder entries — e.g. moving their own Fold to after an opponent's Raise to pretend they folded before seeing it.

**Solution:** Each player knows exactly whose turn it should be at every position in the log, based on the fixed circular order. When verifying an entry, the player checks the signature using the key of whoever *should* have acted at that position. If an entry is out of order, the signature will be verified against the wrong public key and the check will fail. Order is enforced cryptographically, not by trust.

---

### Problem 4: A player could insert a fake entry on behalf of someone else.

A malicious player could try to fabricate an action — e.g. forge a Fold for an opponent who never folded.

**Solution:** Forging an entry requires producing a valid signature under the victim's private key, which the attacker does not have. The verification step will decrypt the signature using the expected player's *public* key and compare it to the hash of the plaintext. A forged entry will fail this check, and the forgery attempt is evident from the log itself.

---

### Problem 5: Players shouldn't need to track the full log at all times.

Requiring every player to stay in sync continuously introduces complexity and bandwidth overhead, especially in a peer-to-peer setting.

**Solution:** Players only need the full log when it is their turn. The log travels with the baton — each player receives it, verifies the new entries since their last turn, acts, and passes it on. Idle players are not burdened with tracking state. When their turn arrives, the log is self-contained and self-verifiable.

---

### Conclusion

Each action is cryptographically bound to the player who made it, at the moment they made it, in the position they were supposed to occupy in the sequence. The log needs no referee — any player can verify the entire history independently using only public keys. Lying, retracting, forging, and reordering are all detectable without trusting any single node.

Because only one player can act at a time, the log simply passes around the circle — no broadcasting, no synchronization overhead. The fully updated log only ever needs to exist in one place at once, reducing the entire action phase to a single continuous relay rather than a flood of network messages. A has the log, then B, then C, then back to A....
