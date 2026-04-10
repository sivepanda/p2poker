# Peer-to-Peer Poker 

A fully decentralized, trustless multiplayer Texas Hold'em poker game over a peer-to-peer network with no central server.

## Overview

Players connect directly to each other and use cryptographic protocols to ensure fair play without a trusted third party. The system handles three core challenges:

- **Trustless Card Dealing** - uses commutative encryption (SRA) with fresh ephemeral keypairs per game. The deck is collectively shuffled and encrypted by all players so no single player controls the card order or can see another's cards. At showdown, players publish their private keys for cryptographic verification.

- **Action Logging & Round Consistency** - every player holds a full replica of the game log. Each action is signed over the entire log history so that a single signature simultaneously proves identity, consistency, and integrity. Divergence is detected immediately on the next turn rather than at audit boundaries. See [the consistency whitepaper](docs/whitepaper/consistency.md) for details.

- **Liveness & Fault Tolerance** - configurable timeouts detect unresponsive players, who are auto-folded after repeated failures to prevent stalling.

## Key Properties
- **No central server** - all game logic is executed and verified by every participant
- **Turn-based simplicity** - one player acts at a time, so ordering is straightforward
- **Forward secrecy** - ephemeral keypairs per game ensure past sessions cannot be decrypted if a long-term key is compromised
- **Tamper-evident** - any attempt to forge, replay, reorder, or deny actions is cryptographically detectable
- **UI Plugin Interface** - the core service is frontend-agnostic. An API contract lets any client render the game, so anyone can write their own frontend.

## Project Structure

Follows Go conventions inspired by Kubernetes and Docker layouts. See [docs/package_structure.md](docs/package_structure.md) for details.

```
cmd/
  dispatch/    # Discovery server for peer lookups
  node/        # Single player's peer process
  sim/         # Local game simulation
internal/
  crypto/      # Cryptographic primitives (deck & log)
  dispatch/    # Dispatch server logic
  peer/        # Peer networking and mesh
  protocol/    # Wire format and frame definitions
  sim/         # Simulation logic
  transport/   # Network transport layer
```

## Running

### Simulation

```sh
make sim
# or
go run ./cmd/sim
```

### Dispatch + Nodes (P2P Framework)

```sh
# Terminal 1 - start the dispatch server
go run ./cmd/dispatch -addr :9000

# Terminal 2 - create a session and join as the first node
go run ./cmd/node -dispatch 127.0.0.1:9000 -create -session table-1 -list-peers

# Terminal 3 - join the existing session as a second node
go run ./cmd/node -dispatch 127.0.0.1:9000 -session table-1 -list-peers

# Send a message from one node to another
go run ./cmd/node -dispatch 127.0.0.1:9000 -session table-1 -send-to node-2 -body "hello from node-1"
```

### Build All

```sh
make build   # produces ./app, ./dispatch, ./node binaries
make clean   # removes built binaries
```

See [docs/running.md](docs/running.md) for full flag reference.

## Planning & Design

- [Trustless Card Dealing Algorithm](docs/whitepaper/dealing.md)
- [Round Consistency](docs/whitepaper/consistency.md)


*This project was made as a final project for Computer Science 390.03, Distributed Systems at Duke University, taught by Dr. Jeff Chase in Spring 2026.*  
**Siven Panda, Ahbab Abeer, Chirag Biswas**
