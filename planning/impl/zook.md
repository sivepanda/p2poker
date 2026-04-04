# Round Consistency

In our current state, we have two potential options for holding round consistency, each draws from a different portions of the class. In one case, we use DNSSEC style signing of events such that any one user can verify that a particular move was made by a particular player, and a state that is shared round-robin, and rotated around as players make moves. In the other case, we have a Zookeeper-stlye log format where each player stores a complete copy of the game log up until that point, then must reach a consensus on any new changes being made.


## Zooky-Inspired
The Zooky-inspired implementation uses the backbone of having replicated states that reach eventual consistency. Essentially, a "game log" is stored that contains all checks, raises, calls, and folds on any given turn. 

Each turn is assigned an ID equivalent to the turn counter (so the nth turn has ID n). When it is a given player's turn, all players "listen" to an epiphermial file hosted by the player whose turn it is. Once the player "proposes" their change, all players then host a `verify-[round-id]` epiphermial file, which will only exist if they successfully "stage" this proposal to their game log. Once all players can mutually verify that they have individually staged the proposal (they all carry the `verify-[round-id] file`), all players then "commit" this change to their log..

The benefit to this approach is that all players can mutually verify that a particular change is valid, and that it is impossible for any one player to tamper with the history of the logs.

---

## Symmetric Replicated Log (Zookeeper-Inspired) — Full Specification

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

Turn order is deterministic, derived from the current committed log state. Because of this, only one player can validly propose at any given time — a proposal is only considered valid if its round ID matches the current log length and its player ID matches the expected next player. If a player attempts to propose out of turn or submits an illegal action, other players will simply not host verify files for it, and the round will never reach commit. Mutual exclusion is enforced at the application layer without the need for a locking mechanism.

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

Optionally, at natural game boundaries (such as the end of a hand), players can perform a consistency audit. Each player computes a keyed hash over their full serialized log using a pre-shared key and publishes the resulting digest. If all digests match, we can confirm that every player's log is identical without needing to exchange and diff full logs. This is primarily useful for catching accidental state divergence — corrupted writes, missed commits, validation bugs — rather than adversarial tampering, since the key is shared amongst all players.

```
audit_consistency(local_log, shared_key):
    digest = hmac(shared_key, serialize(local_log))
    publish("audit-" + current_hand_id, digest)
    
    all_digests = collect_all_audit_files(current_hand_id, player_count)
    
    for d in all_digests:
        if d != digest:
            raise ConsistencyError("log divergence detected")
```

### Adversarial Considerations

Because all players hold independent replicas of the log, the attack surface is fundamentally different from a centralized model. There is no single source of truth to compromise — the integrity of the game state is derived from mutual agreement across all players.

#### Log Tampering

A player who tampers with their own local log gains nothing from doing so. Their modified state will diverge from every other player's log, and subsequent proposals or verifications will fail because their view of the game no longer matches incoming proposals. Effectively, tampering with your own log is indistinguishable from corruption — the player will fail validation checks and eventually be treated as disconnected.

```
// player 2 attempts to rewrite their log to remove a previous raise
player_2_log[4].action = Call    // was Raise(100)

// on the next round, player 2 receives a proposal:
validate(proposal, player_2_log):
    // player 2's game_state(log) now reflects a different pot size,
    // different stack sizes, possibly a different turn order.
    // validation will fail or produce incorrect verify files
    // that won't match other players' digests.
```

A player also cannot tamper with any other player's log, since they do not have access to it. This is the core architectural advantage of full replication — there is no shared mutable state to attack.

#### Log Swap Attacks

A more sophisticated attack would be for a player to maintain two versions of the log — a "real" one they use to participate in the protocol correctly, and a "fake" one they present during disputes or auditing. However, this is mitigated by the [consistency audit mechanism](#log-consistency-auditing). Since the keyed hash is computed over the entire serialized log, a player cannot present a different log without producing a different digest. As long as the audit runs at regular checkpoints, a swap would be detected at the next boundary.

That said, because the audit key is shared, a malicious player who knows what the "correct" log looks like could compute the correct digest from the real log while locally holding a modified copy. In practice, this is not useful — the modified copy would only cause them to fail future validations, and the digest they published would still confirm they had the correct log at audit time. The attack is self-defeating.

#### Collusion

The protocol does not protect against collusion. If a majority of players cooperate, they can collectively agree to verify fraudulent proposals — for example, committing a fold for a player who actually raised. From the perspective of the protocol, the verify files are all present and consistent, so the commit proceeds. This is a fundamental limitation of any consensus system where the majority is trusted, and mirrors the same constraint in Raft (a majority of nodes can elect an arbitrary leader and commit arbitrary entries). In a poker context, collusion is generally treated as a social problem rather than a protocol one.

#### Replay Attacks

Because each proposal includes a round ID that must be strictly sequential with the log, replaying a previous proposal will always fail validation. A replayed proposal will carry a stale `round_id` that no longer matches `len(log)`, and will be rejected during the verify phase. Similarly, because `player_id` must match the expected turn order derived from the current log state, a replayed action from a previous turn will almost certainly fail the turn order check as well.

```
// attacker replays proposal from round 3 during round 7
replayed_proposal = { round_id: 3, player_id: 1, action: Raise(50) }

validate(replayed_proposal, log):
    // len(log) == 7, proposal.round_id == 3
    // immediate rejection
    return false
```

#### Sybil Attacks

If a player is able to join the game session under multiple identities, they could control multiple seats and manipulate the game through coordinated play. The protocol itself does not handle identity verification — this is deferred to the session establishment layer. At game creation, all players must agree on the set of participants, and no new players can join mid-hand. How identity is verified (shared secret, public key exchange, trusting the network layer) is an implementation decision outside the scope of the replication protocol.

### Advantage: No Single Point of Log Failure

In a baton-passing model, the log exists in exactly one place at a time — whoever currently holds the baton. If that player goes offline, crashes, or loses their connection while holding the log, the entire game state is lost or stalled. Every other player is idle and has no copy of the most recent actions. Recovery requires the baton holder to come back, or the game must be abandoned.

Because every player maintains a full replica of the log at all times, no single disconnection can destroy the game state. If the active player drops, the remaining players still hold identical, up-to-date copies of the committed log. The timeout mechanism handles the abort, and the game can continue from the last committed state. The log is never in transit — it is always committed everywhere simultaneously, so there is no window during which the game state exists on only one machine.

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

The replicated log model trades bandwidth for resilience. Every player pays the cost of storing and validating the full log on every turn, but in return, no single node is a bottleneck or a point of failure. Game state is never in flight — it is always committed everywhere or nowhere. Integrity is enforced through mutual verification rather than cryptographic signatures: a proposal only becomes part of the log when every player independently agrees it is valid. Tampering is self-defeating, replays are structurally impossible, and unresponsive players are gracefully removed rather than allowed to stall the game.
