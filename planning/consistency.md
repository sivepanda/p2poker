# Round Consistency

In our current state, we have two potential options for holding round consistency, each drawing from different portions of the class. In one case, we use DNSSEC-style signing of events such that any one user can verify that a particular move was made by a particular player, and a state that is shared round-robin and rotated around as players make moves. In the other case, we have a Zookeeper-style log format where each player stores a complete copy of the game log up until that point, then must reach a consensus on any new changes being made.

## Table of Contents

- [Method 1: Symmetric Replicated Log (Zookeeper-Inspired)](#method-1-symmetric-replicated-log-zookeeper-inspired)
  - [Setup](#setup)
  - [Turn Lifecycle](#turn-lifecycle)
  - [Verification and Commit](#verification-and-commit)
  - [Ordering and Mutual Exclusion](#ordering-and-mutual-exclusion)
  - [Timeout and Disconnect Handling](#timeout-and-disconnect-handling)
  - [Log Consistency Auditing](#log-consistency-auditing)
  - [Problems & How This Algorithm Solves Them](#problems--how-this-algorithm-solves-them)
  - [Full Flow Summary](#full-flow-summary)
  - [Conclusion](#conclusion)
- [Method 2: Signed Hot-Potato Log (DNSSEC-Inspired)](#method-2-signed-hot-potato-log-dnssec-inspired)
  - [Setup](#setup-1)
  - [Structure of the Log](#structure-of-the-log)
  - [Phase: Taking a Turn](#phase-taking-a-turn)
  - [Full Flow Summary](#full-flow-summary-1)
  - [Problems & How This Algorithm Solves Them](#problems--how-this-algorithm-solves-them-1)

---

## Method 1: Symmetric Replicated Log (Zookeeper-Inspired)

The Zookeeper-inspired implementation uses the backbone of having replicated states that reach eventual consistency. Essentially, a "game log" is stored by every player that contains all checks, raises, calls, and folds on any given turn. Each turn is assigned an ID equivalent to the turn counter (so the nth turn has ID n). Every player maintains a full local replica of the log at all times, and changes are committed only after all players mutually verify each proposal.

### Setup

Players establish a fixed circular order `A → B → C → A` at game creation time. Each player's identity is agreed upon by all participants before the first hand begins, and the session is created and allocated by the host. All players share a pre-established communication channel over which they can host and observe ephemeral files (proposals, verify signals, audit digests). A pre-shared key is optionally distributed at session setup for use in [log consistency auditing](#log-consistency-auditing). Each player initializes an empty local log and a turn counter set to zero. Turn order is deterministic and derived from the log state, so no additional coordination is needed to determine who acts first.

### Turn Lifecycle

Each turn proceeds through three phases: **propose**, **verify**, and **commit**. When it is a player's turn, they host a proposal file that contains the round ID (equivalent to the current length of the log), their player ID, the action (fold, check, call, raise), and an amount where applicable. All other players listen for this file, much like how clients watch an ephemeral znode for changes in Zookeeper.

A proposal takes the following structure:

```
Proposal {
    round_id:   u64,
    player_id:  u8,
    action:     enum { Fold, Check, Call, Raise(amount) },
    amount:     u64,
    timestamp:  u64
}
```

### Verification and Commit

Once a player observes the proposal, they validate it against their own local copy of the game state. This means checking that the round ID is sequential with their log, that it is in fact the proposing player's turn, and that the action is legal given the current bet and the player's stack.

```
validate(proposal, log):
    if proposal.round_id != len(log):
        return false
    if proposal.player_id != expected_next_player(log):
        return false
    if not is_legal_action(proposal.action, proposal.amount, game_state(log)):
        return false
    return true
```

If validation passes, the player hosts a `verify-[round_id]` ephemeral file. Once all players can mutually observe that every other player has published their verify file for that round, all players append the proposal to their local log and increment the turn counter. The ephemeral proposal and verify files for that round are then discarded.

```
on_proposal_received(proposal):
    if validate(proposal, local_log):
        publish("verify-" + proposal.round_id)
    
    await_all_verify_files(proposal.round_id, player_count)
    
    local_log.append(proposal)
    turn_counter += 1
    cleanup_ephemeral_files(proposal.round_id)
```

### Ordering and Mutual Exclusion

Turn order is deterministic. The intial order of players is determined during the setup of the game, and the current player is derived from the current committed log state. Because of this, only one player can validly propose at any given time, a proposal is only considered valid if its round ID matches the current log length and its player ID matches the expected next player. If a player attempts to propose out of turn or submits an illegal action, other players will simply not host verify files for it, and the round will never reach commit. Mutual exclusion is enforced at the application layer without the need for a locking mechanism.

### Timeout and Disconnect Handling

If a player fails to publish a verify file within a configurable timeout window, the round enters an abort state. The staged proposal is discarded and the active player may re-propose. After some number of consecutive failed rounds from a single player, the remaining players treat that seat as folded for the remainder of the hand. This ensures that a single unresponsive client cannot stall the game indefinitely.

```
await_all_verify_files(round_id, player_count):
    attempts = 0
    while not all_verify_files_present(round_id, player_count):
        wait(TIMEOUT_INTERVAL)
        attempts += 1
        if attempts >= MAX_ATTEMPTS:
            abort_round(round_id)
            return false
    return true

abort_round(round_id):
    discard_staged(round_id)
    consecutive_failures[active_player] += 1
    if consecutive_failures[active_player] >= K:
        fold_player(active_player)
```

### Log Consistency Auditing

Optionally, at natural game boundaries (such as the end of a hand), players can perform a consistency audit. Each player computes a keyed hash over their full serialized log using a pre-shared key and publishes the resulting digest. If all digests match, we can confirm that every player's log is identical without needing to exchange and diff full logs. This is primarily useful for catching accidental state divergence: corrupted writes, missed commits, validation bugs, rather than adversarial tampering, since the key is shared amongst all players. This could also be implemented as a secondary verification for a commit in [log consistency auditing](#log-consistency-auditing), where, like in Cryptocurrency, a "header" is appened to a proposed commit that contains the proposer's keyed hash, then verified by every other player before committing that particular move.

```
audit_consistency(local_log, shared_key):
    digest = hmac(shared_key, serialize(local_log))
    publish("audit-" + current_hand_id, digest)
    
    all_digests = collect_all_audit_files(current_hand_id, player_count)
    
    for d in all_digests:
        if d != digest:
            raise ConsistencyError("log divergence detected")
```

### Problems & How This Algorithm Solves Them

Because all players hold independent replicas of the log, the attack surface is fundamentally different from a centralized model. There is no single source of truth to compromise: the integrity of the game state is derived from mutual agreement across all players.

---

#### Problem 1: A player could tamper with their own log to change the game history.

A player might rewrite a past entry in their local log, for example, changing a Raise to a Call, to alter pot sizes, stack sizes, or turn order in their favor.

**Solution:** Tampering with your own log is self-defeating. The modified state will diverge from every other player's replica, and subsequent proposals or verifications will fail because the tampered log no longer matches incoming proposals. The player will fail validation checks and eventually be treated as disconnected. A player also cannot tamper with any other player's log, since they have no access to it.

---

#### Problem 2: A player could deny having taken an action.

After a proposal is committed, a player might try to claim they never made that action: for example, denying a raise that put them in a bad position.

**Solution:** During live play, repudiation is not viable. Every player holds an identical copy of the committed log, and the [consistency audit](#log-consistency-auditing) confirms all replicas match via HMAC digest. If a player denies an entry, every other participant's log contradicts them. The shared log *is* the proof: a player cannot disown an action that every other player independently validated and committed. Unlike signature-based schemes where proof is bound to a single entry, here proof is structural: it is the unanimous agreement of the entire network.

---

#### Problem 3: A player could maintain a fake log to present during disputes or auditing.

A more sophisticated attack would be for a player to keep two versions of the log: a "real" one they use to participate in the protocol correctly, and a "fake" one they present during audits or disputes.

**Solution:** The [consistency audit mechanism](#log-consistency-auditing) computes a keyed hash over the entire serialized log. A player cannot present a different log without producing a different digest, so a swap would be detected at the next audit boundary. Because the audit key is shared, a malicious player who knows the "correct" log could compute the correct digest while locally holding a modified copy  but in practice this is useless, since the modified copy would only cause them to fail future validations. The attack is self-defeating.

---

#### Problem 4: A player could forge a proposal on behalf of another player.

A malicious player might try to host a proposal file impersonating another player ()for example, submitting a fold on behalf of an opponent who never folded)

**Solution:** Players are verified by-network during session establishment. Each player is bound to a known address (IP/port) before the game begins, and all peers know which network endpoint corresponds to which seat. When listening for a proposal, players only accept files from the endpoint of the player whose turn it is. A proposal arriving from any other source is ignored regardless of its contents. Forgery would require compromising the network identity of the target player, which is outside the scope of the replication protocol.

---

#### Problem 5: A player could replay a previous proposal to disrupt the game.

An attacker might try to re-submit a proposal from an earlier round to confuse other players or force an invalid state transition.

**Solution:** Each proposal includes a `round_id` that must be strictly sequential with the log. A replayed proposal will carry a stale `round_id` that no longer matches `len(log)` and will be immediately rejected during validation. Similarly, because `player_id` must match the expected turn order derived from the current log state, a replayed action from a previous turn will fail the turn order check as well.

```
// attacker replays proposal from round 3 during round 7
replayed_proposal = { round_id: 3, player_id: 1, action: Raise(50) }

validate(replayed_proposal, log):
    // len(log) == 7, proposal.round_id == 3
    // immediate rejection
    return false
```

---

#### Problem 6: A majority of players could collude to commit fraudulent actions.

If a majority of players cooperate, they could collectively agree to verify fraudulent proposals ()for example, committing a fold for a player who actually raised)

**Solution:** The protocol does not protect against this. From the perspective of the protocol, if the verify files are all present and consistent, the commit proceeds. This is a fundamental limitation of any consensus system where the majority is trusted, and mirrors the same constraint in Raft (a majority of nodes can elect an arbitrary leader and commit arbitrary entries). In a poker context, collusion is generally treated as a social problem rather than a protocol one.

---

#### Problem 7: A player could join under multiple identities to control multiple seats.

If a player is able to join the game session under multiple identities, they could manipulate the game through coordinated play across multiple seats.

**Solution:** The protocol defers identity verification to the session establishment layer. At game creation, all players must agree on the set of participants, and no new players can join mid-hand. How identity is verified (shared secret, public key exchange, trusting the network layer) is an implementation decision outside the scope of the replication protocol.

---

#### Problem 8: The log could be lost if the wrong player disconnects.

In [Method 2](#method-2-signed-hot-potato-log-dnssec-inspired), the log exists in exactly one place at a time: whoever currently holds the flag. If that player goes offline, crashes, or loses their connection while holding the log, the entire game state is lost or stalled. Every other player is idle and has no copy of the most recent actions.

**Solution:** Because every player maintains a full replica of the log at all times, no single disconnection can destroy the game state. If the active player drops, the remaining players still hold identical, up-to-date copies of the committed log. The [timeout mechanism](#timeout-and-disconnect-handling) handles the abort, and the game can continue from the last committed state. The log is never in transit: it is always committed everywhere simultaneously, so there is no window during which the game state exists on only one machine.

---

### Full Flow Summary

```
SETUP         All players initialize empty log, agree on player set and turn order

PROPOSE       Active player hosts proposal file (round_id, player_id, action, amount)

VALIDATE      All other players observe proposal and validate against local log:
                - round_id == len(local_log)
                - player_id == expected_next_player
                - action is legal given current game state

VERIFY        Each player who passes validation hosts a verify-[round_id] file

COMMIT        Once all verify files are mutually observed:
                - All players append proposal to local log
                - Increment turn counter
                - Discard ephemeral proposal and verify files

TIMEOUT       If verify files are not all present within the timeout window:
                - Abort round, discard staged proposal
                - Track consecutive failures per player
                - Auto-fold unresponsive players after K failures

AUDIT         (Optional) At hand boundaries:
                - Each player publishes HMAC digest of their full log
                - Compare digests to detect divergence
```

### Conclusion

The replicated log model trades bandwidth for resilience. Every player pays the cost of storing and validating the full log on every turn, but in return, no single node is a bottleneck or a point of failure. Game state is never in flight: it is always committed everywhere or nowhere. Integrity is enforced through mutual verification rather than cryptographic signatures: a proposal only becomes part of the log when every player independently agrees it is valid. Tampering is self-defeating, replays are structurally impossible, and unresponsive players are gracefully removed rather than allowed to stall the game.

---

## Method 2: Signed Hot-Potato Log (DNSSEC-Inspired)

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

### Problems & How This Algorithm Solves Them

---

#### Problem 1: A player could lie about what action they took.

A player might claim they folded when they actually raised, or deny having acted at all.

**Solution:** Every action is signed with the acting player's ephemeral private key. Since only that player holds their private key, a valid signature is proof that they and only they produced that entry. A player cannot disown a log entry that bears a valid signature from their own key — "I didn't make that action" is not a viable defence.

---

#### Problem 2: A player could try to take back an action after seeing new information.

After acting, a player might want to retroactively change their entry — e.g. switch a Call to a Fold after seeing an opponent's behaviour.

**Solution:** The log is append-only and each entry is signed at the moment of action. Once a signed entry is in the log and the next player has received it, it cannot be altered without invalidating the signature. Any modification to the plaintext produces a different hash, which no longer matches the stored signature. The tamper is immediately detectable.

---

#### Problem 3: A player could try to swap or reorder entries to change the game state.

A malicious player might try to reorder entries — e.g. moving their own Fold to after an opponent's Raise to pretend they folded before seeing it.

**Solution:** Each player knows exactly whose turn it should be at every position in the log, based on the fixed circular order. When verifying an entry, the player checks the signature using the key of whoever *should* have acted at that position. If an entry is out of order, the signature will be verified against the wrong public key and the check will fail. Order is enforced cryptographically, not by trust.

---

#### Problem 4: A player could insert a fake entry on behalf of someone else.

A malicious player could try to fabricate an action — e.g. forge a Fold for an opponent who never folded.

**Solution:** Forging an entry requires producing a valid signature under the victim's private key, which the attacker does not have. The verification step will decrypt the signature using the expected player's *public* key and compare it to the hash of the plaintext. A forged entry will fail this check, and the forgery attempt is evident from the log itself.

---

#### Problem 5: Players shouldn't need to track the full log at all times.

Requiring every player to stay in sync continuously introduces complexity and bandwidth overhead, especially in a peer-to-peer setting.

**Solution:** Players only need the full log when it is their turn. The log travels with the baton — each player receives it, verifies the new entries since their last turn, acts, and passes it on. Idle players are not burdened with tracking state. When their turn arrives, the log is self-contained and self-verifiable.

---

#### Conclusion

Each action is cryptographically bound to the player who made it, at the moment they made it, in the position they were supposed to occupy in the sequence. The log needs no referee — any player can verify the entire history independently using only public keys. Lying, retracting, forging, and reordering are all detectable without trusting any single node.

Because only one player can act at a time, the log simply passes around the circle — no broadcasting, no synchronization overhead. The fully updated log only ever needs to exist in one place at once, reducing the entire action phase to a single continuous relay rather than a flood of network messages. A has the log, then B, then C, then back to A....
