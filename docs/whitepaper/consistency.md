# Round Consistency

In earlier iterations of this protocol, we explored two distinct approaches to round consistency. The first, a Zookeeper-inspired replicated log, gave every player a full copy of the game state and enforced integrity through mutual verification — but relied on network-layer identity, meaning a compromised endpoint could forge proposals without cryptographic detection. The second, a DNSSEC-inspired signed hot-potato log, bound every action to a cryptographic identity — but concentrated the game state in a single traveling copy, making it fragile to disconnections.

The hybrid approach described here fuses these two models. Every player holds a full replica of the log at all times, and every action is cryptographically signed — not in isolation, but over the entire log history. The consistency check and the identity check collapse into a single operation, eliminating the gap between "is this the right person?" and "do we agree on what happened?"

## Table of Contents

- [Design Rationale](#design-rationale)
- [Setup](#setup)
- [Log Structure](#log-structure)
- [Turn Lifecycle](#turn-lifecycle)
- [Verification](#verification)
- [Timeout and Disconnect Handling](#timeout-and-disconnect-handling)
- [Problems & How This Algorithm Solves Them](#problems--how-this-algorithm-solves-them)
- [Full Flow Summary](#full-flow-summary)
- [Tradeoffs](#tradeoffs)

---

## Design Rationale

The core insight is that a cryptographic signature can do more work than just proving identity. If the signed payload includes not just the action but the hash of the entire log up to that point, then verifying the signature simultaneously proves three things:

1. **Identity** - only the holder of the private key could have produced the signature.
2. **Consistency** - the signer's log matches the verifier's log, because the hash would differ otherwise.
3. **Integrity** - the action has not been tampered with in transit.

In the replicated log model, consistency was enforced through a separate verify-file exchange, and identity was deferred to the network layer. In the signed hot-potato model, identity was cryptographically guaranteed but consistency was only checked when the log arrived at your door. Here, both properties are proven on every single turn, by every player, as a single atomic operation. There is no separate audit step, the audit is built into the signature.

---

## Setup

Players establish a fixed circular order `A -> B -> C -> A` at game creation time. Each player generates a fresh ephemeral keypair for the session, bound to their long-term identity (the same keypairs used during the [card dealing phase](dealing.md)). Public keys are distributed to all peers before the first hand begins.

Each player initializes an empty local log and a turn counter set to zero. Turn order is deterministic and derived from the log state, so no additional coordination is needed to determine who acts next.

---

## Log Structure

The log is an ordered list of entries. Each entry is a tuple of the plaintext action, the signature over the cumulative log, and the signer's player ID:

```
Entry {
    round_id:   u64,
    player_id:  u8,
    action:     enum { Fold, Check, Call, Raise(amount) },
    amount:     u64,
    signature:  bytes
}
```

The signature is computed as:

```
signature = sign(hash(log || action), private_key)
```

Where `log` is the serialized committed log at the time of signing and `action` is the serialized new action. The `||` operator denotes concatenation. The hash function produces a fixed-size digest over the entire payload, so the signature covers both the full history and the new action as a single unit.

This is the structural difference from both prior methods. In Method 1, the log carried no cryptographic binding — entries were just data, and trust came from mutual verification files. In Method 2, each entry was signed independently over its own action, so the signature proved authorship but said nothing about the signer's view of history. Here, the log digest is baked into every signature, making each entry a cryptographic commitment to the entire game state as the signer understood it at that moment.

---

## Turn Lifecycle

Each turn proceeds through three phases: **propose**, **verify**, and **commit**.

### Propose

When it is a player's turn, they compute the hash of their full local log concatenated with their chosen action, sign that hash with their ephemeral private key, and broadcast a proposal to all peers:

```
propose(action, local_log, private_key):
    payload = serialize(local_log) || serialize(action)
    sig = sign(hash(payload), private_key)
    broadcast(Proposal {
        round_id:   len(local_log),
        player_id:  self.id,
        action:     action,
        amount:     action.amount,
        signature:  sig
    })
```

### Verify

When a player receives a proposal, they perform a single verification that simultaneously checks identity, consistency, and integrity:

```
verify(proposal, local_log):
    // check sequencing and turn order
    if proposal.round_id != len(local_log):
        return false
    if proposal.player_id != expected_next_player(local_log):
        return false
    if not is_legal_action(proposal.action, proposal.amount, game_state(local_log)):
        return false

    // the unified check: rebuild what the signer should have signed
    expected_payload = serialize(local_log) || serialize(proposal.action)
    expected_hash = hash(expected_payload)

    // verify the signature using the proposer's public key
    if not verify_signature(proposal.signature, expected_hash, proposer.public_key):
        return false

    return true
```

The `verify_signature` call is doing all the heavy lifting. If the proposer's log differs from ours by even a single bit, `expected_hash` will not match the hash they signed, and the signature check fails. If someone other than the claimed player produced the signature, the public key will not match. If the action was modified in transit, the hash will not match. One check, three guarantees.

### Commit

If verification passes, the player broadcasts a `verify-[round_id]` signal. Once all players have observed that every peer has published their verify signal for that round, all players append the proposal to their local log and increment the turn counter. The ephemeral proposal and verify signals are then discarded.

```
on_proposal_received(proposal):
    if verify(proposal, local_log):
        broadcast("verify-" + proposal.round_id)

    await_all_verify_signals(proposal.round_id, player_count)

    local_log.append(proposal)
    turn_counter += 1
```

---

## Verification

Because part of each signagure includes a hash over the log, consistency verification happens on every turn automatically. This is an improvement over both initial legacy models. There is no longer a need for periodic HMAC-based audits at each hand boundary like the pure replicated log model).  
  
This also enables immediate detection of log divergence. Once an action occurs, a step of committing any action is a verification of log consistency, so it will surface any sort of corrupted write or commit as soon as anyone acts/

The verification is also non-repudiable. In the replicated log model, proof of an action was structural ("everyone's log says you did it") which is strong during live play but weaker after the fact, since logs are unsigned data. Here, every entry carries a signature that anyone can verify independently, at any time, using only the public key and the log. A player cannot disown an action whose signature they produced.

---

## Timeout and Disconnect Handling

If a player fails to broadcast a verify signal within a configurable timeout window, the round enters an abort state. The staged proposal is discarded and the active player may re-propose. After a threshold of consecutive failed rounds from a single player, the remaining players treat that seat as folded for the remainder of the hand.

```
await_all_verify_signals(round_id, player_count):
    attempts = 0
    while not all_verify_signals_present(round_id, player_count):
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

### Auto-fold attestations

`fold_player(active_player)` cannot be a single-signer entry, as the target, by definition, is the one failing to act, and Problem 3 forbids any other player from forging a fold under the target's key. Instead, once a node has locally observed K consecutive aborted attempts from the same seat, every non-folded seat **other than** the target acts as an attestor. Attestations are conditional: no attestation file is produced during normal play, during individual aborted attempts, or for rounds that commit successfully. They are only hosted once the K-failure threshold is crossed on that replica.

Each attestor signs:

```
attestation_payload = serialize(local_log) || "auto_fold" || round_id || target_seat
attestation_sig     = sign(hash(attestation_payload), attestor_private_key)
```

and hosts the signature as an ephemeral file keyed by `(round_id, attestor_id)`. Every replica — including the target — polls the expected attestor set (derived deterministically from replayed state as "non-folded seats minus target") and assembles the same entry:

```
Entry {
    round_id:      u64,
    player_id:     u8,          // target seat
    action:        Fold,
    amount:        0,
    signature:     [],          // empty: distinguishes auto-fold from self-signed fold
    co_signers:    [u8],        // sorted attestor seats
    co_signatures: [[u8]]       // same length/order as co_signers
}
```

Verification accepts the entry only if every expected attestor's co-signature verifies against its public key under the fixed payload. The entry's serialization includes the co-signer section when `signature` is empty, so the hash chain that binds future proposals still commits to the full attestor set.

This preserves the Problem 3 guarantee for the minority-attacker case: a lone attacker cannot forge an auto-fold because they cannot produce the N−2 other attestors' signatures. It does not strengthen the protocol against majority collusion, but that is an inherent problem with Poker as a game, as discussed in Problem 6. If a single expected attestor never publishes (because of clock skew, byzantine refusal, or its own disconnection) the auto-fold consensus does not complete and the round hangs on that replica, the same liveness failure the whitepaper already accepts for majority-collusion scenarios.

Because every player holds a full replica of the log, no single disconnection can destroy the game state, which was a concern with the hot potato design. If the active player drops, the remaining players still hold identical, up-to-date copies of the committed log. The game continues from the last committed state. The log is never in transit and never exists in only one place. It is always committed everywhere or nowhere.

---

## Problems & How This Algorithm Solves Them

---

### Problem 1: A player could tamper with their own log to change the game history.

A player might rewrite a past entry in their local log — changing a Raise to a Call, for example — to alter pot sizes, stack sizes, or turn order in their favor.

**Solution:** Tampering with your own log is self-defeating on two levels. First, structurally: a modified log will produce different hashes, causing all subsequent proposals and verifications to fail against every other player's replica. Second, cryptographically: past entries contain signatures computed over the log state at the time they were made. A retroactive change to the log invalidates the hash chain that those signatures commit to. The tampered log is not just inconsistent with other players — it is internally inconsistent with its own signatures.

---

### Problem 2: A player could deny having taken an action.

After a proposal is committed, a player might claim they never made that action — denying a raise that put them in a bad position, for example.

**Solution:** Every action carries a signature produced by the acting player's private key. This signature covers both the action and the full log state at the time of signing. A player cannot disown an entry whose signature they produced — the signature is a cryptographic proof of authorship that anyone can verify independently using only the public key and the log. Unlike the pure replicated log model, where proof was structural ("everyone's log agrees"), here proof is cryptographic and portable: a single signed entry is sufficient evidence, even outside the context of live play.

---

### Problem 3: A player could forge a proposal on behalf of another player.

A malicious player might try to submit an action impersonating another player — for example, submitting a fold on behalf of an opponent who never folded.

**Solution:** Forging an entry requires producing a valid signature under the victim's private key, which the attacker does not have. The verification step checks the signature against the expected player's public key. A forged entry will fail this check immediately. Unlike the pure replicated log model, where identity was enforced at the network layer (binding players to IP/port pairs), here identity is enforced cryptographically. Forgery requires breaking the signature scheme, not just spoofing a network address.

---

### Problem 4: A player could replay a previous proposal to disrupt the game.

An attacker might re-submit a proposal from an earlier round to confuse other players or force an invalid state transition.

**Solution:** Each proposal includes a `round_id` that must be strictly sequential with the log length, and the signature covers the full log state at the time of signing. A replayed proposal carries a stale `round_id` and a signature computed over a shorter log. Even if the `round_id` were somehow corrected, the signature would not match the current log state, because the hash of the current log concatenated with the old action differs from the hash of the old log concatenated with the old action. Replays are structurally and cryptographically impossible.

---

### Problem 5: A player could maintain a fake log to present during disputes.

A player might keep two versions of the log — a "real" one used to participate correctly and a "fake" one presented during disputes or auditing.

**Solution:** Every entry in the log is signed over the cumulative log state. A fake log with even one altered entry will produce a different hash chain, and the signatures in subsequent entries will not verify against it. The log is self-authenticating: given the public keys and the entries, anyone can replay the entire verification sequence and detect any divergence. A player cannot construct a plausible alternative history because they cannot forge other players' signatures over a different log state.

---

### Problem 6: A majority of players could collude to commit fraudulent actions.

If a majority of players cooperate, they could collectively agree to verify fraudulent proposals — for example, committing a fold for a player who actually raised.

**Solution:** The protocol does not fully protect against this. However, the cryptographic signatures provide a stronger defense than the pure replicated log model. A colluding majority can verify a fraudulent proposal, but they cannot forge the victim's signature. If the victim's log shows a different action with a valid signature, the fraud is provable after the fact. The victim holds cryptographic evidence — their own signed entry — that contradicts the committed log. This does not prevent the fraud from occurring in real time, but it makes it provably detectable, which is a meaningful improvement over a system where proof is purely structural.

---

### Problem 7: The log could be lost if the wrong player disconnects.

In a hot-potato model, the log exists in exactly one place at a time. If the holder goes offline, the entire game state is lost.

**Solution:** Every player maintains a full replica of the log at all times. No single disconnection can destroy the game state. The timeout mechanism handles the abort, and the game continues from the last committed state. The log is never in transit — it is always committed everywhere simultaneously, so there is no window during which the game state exists on only one machine.

---

## Full Flow Summary

```
SETUP         All players generate ephemeral keypairs, exchange public keys
              Initialize empty log, agree on player set and turn order

PROPOSE       Active player computes:
                payload = serialize(log) || serialize(action)
                signature = sign(hash(payload), private_key)
              Broadcasts proposal (round_id, player_id, action, signature)

VERIFY        Each player reconstructs the expected payload from their own log:
                expected_hash = hash(serialize(local_log) || serialize(proposal.action))
              Verifies:
                - round_id == len(local_log)
                - player_id == expected_next_player
                - action is legal given current game state
                - verify_signature(signature, expected_hash, proposer.public_key)
              One check proves identity, consistency, and integrity

COMMIT        Each verifying player broadcasts verify-[round_id] signal
              Once all signals observed:
                - Append proposal to local log
                - Increment turn counter
                - Discard ephemeral signals

TIMEOUT       If verify signals not present within timeout window:
                - Abort round, discard staged proposal
                - Track consecutive failures per player
                - Auto-fold unresponsive players after K failures
```

---

## Tradeoffs

The hybrid approach pays the costs of both parent methods. Every player stores and validates the full log (the replication cost), and every player must generate, sign, and verify cryptographic signatures on every turn (the key infrastructure cost). For a small-player poker game, neither cost is meaningful — the log is short, the signatures are fast, and the key management is already handled by the dealing phase.

What is gained is a protocol where consistency and identity are not separate concerns. There is no gap between "do we agree on the game state?" and "is this really you?" — a single signature answers both. Log divergence is detected immediately rather than at audit boundaries. Actions are non-repudiable by construction rather than by social consensus. And the full game state is always available everywhere, so no single point of failure can stall or destroy the game.

The protocol does not protect against a colluding majority, but no decentralized consensus system can. What it does guarantee is that any fraud committed by a minority is cryptographically provable, and any fraud committed by a majority leaves the victim holding signed evidence of the contradiction.
