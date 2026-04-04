# Peer 2 Peer Poker

A fully decentralized, trustless multiplayer Texas Hold'em poker game played over a peer-to-peer network with no central server or authority.

## Overview

Players connect directly to each other in a ring topology and use cryptographic protocols to ensure fair play without relying on a trusted third party. The system handles three core challenges:

- **Trustless Card Dealing** — uses commutative encryption (SRA) with fresh ephemeral keypairs per game so that the deck is collectively shuffled and encrypted by all players. No single player controls the deck order, and no player can see another's cards. At showdown, players publish their private keys to allow cryptographic verification of claimed hands.

- **Action Logging & Round Consistency** — two candidate approaches are under consideration:
  1. **Symmetric Replicated Log (Zookeeper-Inspired)** — every player maintains a full replica of the game log. Actions are proposed, validated, and committed via a mutual verification protocol using ephemeral files. Integrity comes from unanimous agreement rather than a single authority.
  2. **Signed Hot-Potato Log (DNSSEC-Inspired)** — the game log travels around the ring with each turn. Every action is cryptographically signed by its author using ephemeral keys, allowing any player to independently verify the entire history. Lighter on bandwidth since only one player holds the log at a time.

- **Liveness & Fault Tolerance** — configurable timeouts detect unresponsive players, who are auto-folded after repeated failures to prevent stalling.

## Key Properties

- **No central server** — all game logic is executed and verified by every participant
- **Turn-based simplicity** — only one player acts at a time, so ordering is straightforward and concurrency is limited
- **Forward secrecy** — ephemeral keypairs per game ensure past sessions cannot be decrypted if a long-term key is compromised
- **Tamper-evident** — any attempt to forge, replay, reorder, or deny actions is cryptographically detectable

## Planning & Design

Detailed algorithm specifications and security analysis can be found in the planning directory:

- [Trustless Card Dealing Algorithm](planning/dealing.md)
- [Round Consistency Methods](planning/consistency.md)
