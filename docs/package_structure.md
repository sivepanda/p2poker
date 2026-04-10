# Package Structure and Information
Packages are designed and structured modeling the way Kubernetes and Docker are implemented, as well as using suggestions and conventions from [this](https://go.dev/doc/modules/layout) article from the Go Documentation.

# `/cmd`
This package holds **commands** - all executables (i.e. all `main` packages) live here.

## `/cmd/sim`
Runs a local-only simulation of the SRA cryptographic shuffle and deal protocol. Spins up N in-memory nodes that pass a deck around a ring of channels, demonstrating the full shuffle-encrypt-deal pipeline without any networking.

## `/cmd/dispatch`
Runs the Dispatch Server. This is a central discovery service where nodes register and look up peer addresses. It manages sessions and tracks node liveness via leases, but has no role in game logic itself. Configurable listen address and lease TTL via flags.

## `/cmd/node`
Runs a single peer node. Registers with the dispatch server, creates or joins a session, establishes direct TCP connections to other peers in the session, and supports sending/broadcasting messages. Maintains a background heartbeat to keep its lease alive. Optionally starts a gRPC server via `--rpc-addr` so that frontend UI applications can connect and interact with the node.

# `/internal`
Internal packages containing the actual implementation. Not importable by external modules.

## `/internal/crypto`
Cryptographic primitives used by the poker protocol.

### `/internal/crypto/deck`
SRA (commutative encryption) implementation for the card deck. Provides key generation over a safe prime, modular-exponentiation encrypt/decrypt, and a cryptographically-secure Fisher-Yates shuffle. Each player generates an independent `Key` (exponent + inverse) and the deck passes through every player's encrypt step so that no single party controls the card order.

### `/internal/crypto/log`
Ed25519-based signing and verification for action log entries. Each `Entry` contains an author ID, action string, and signature. Provides helpers to generate ephemeral signer keypairs, sign actions (SHA-256 hash + Ed25519), and verify entries against a public key.

## `/internal/dispatch`
Server-side implementation of the dispatch service. Manages a map of registered `nodeConn` records and `session` groups. Handles the full frame-based protocol: registration, session create/join, peer listing, heartbeats, and lease expiry reaping. Runs a background goroutine that disconnects nodes whose leases have expired.

## `/internal/peer`
Client-side peer networking. The `Node` type is the primary interface: it connects to the dispatch server, manages session membership, and builds a direct TCP mesh with other peers in the same session.

Key files:
- **node.go** - `Connect()` constructor, dispatch registration, peer listener setup, and lifecycle management.
- **dispatch.go** - dispatch-facing operations: `CreateSession`, `JoinSession`, `ListPeers`, `Heartbeat`, and the request/response multiplexer over the dispatch connection.
- **mesh.go** - peer-to-peer mesh: `ConnectToPeers` dials all known peers, `acceptPeers` handles inbound connections, and `peerReadLoop` dispatches incoming messages to registered handlers.
- **message.go** - `Send`, `Broadcast`, typed message `Handle` registration, and gob-based payload encode/decode via the generic `Decode[T]` helper.

## `/internal/clientrpc`
gRPC server that exposes `peer.Node` methods to frontend UI applications over a local port. Every RPC is a one-line passthrough — the server holds no game logic. `SubscribeEvents` is a server-side stream that pushes incoming P2P messages to the connected frontend in real time.

### `/internal/clientrpc/clientrpcpb`
**THIS PACKAGE IS ENTIRELY GENERATED CODE — DO NOT EDIT IT BY HAND.** It is produced by `protoc` from the Protocol Buffer definition at `proto/clientrpc/v1/clientrpc.proto`. Contains message types (`*.pb.go`) and the gRPC server/client interfaces (`*_grpc.pb.go`). Committed to the repo so `go build` works without `protoc` installed. **To change anything here, modify the `.proto` file and regenerate with `make proto`.**

# `/proto`
Protocol Buffer definitions. These `.proto` files are the canonical API contracts — frontend authors in any language generate their client stubs from them.

## `/proto/clientrpc/v1`
Defines the `PokerNode` gRPC service: lobby operations (`CreateSession`, `JoinSession`, `ListPeers`, `ConnectPeers`), node info (`GetNodeInfo`), and a server-streaming `SubscribeEvents` RPC for real-time event push. Game-specific RPCs will be added here as the game engine is built.

## `/internal/protocol`
Defines the `Frame` struct — the universal wire format for all communication (both dispatch and peer-to-peer). Also declares `Kind` constants for every frame type: register, session create/join, peer list, and heartbeat request/response pairs.

## `/internal/sim`
Local simulation engine. The `Network` type sets up a grid of Go channels to simulate inter-node communication without TCP. Implements the full SRA shuffle relay (leader initiates, each follower shuffles and re-encrypts, leader broadcasts the final deck) and the ring-based dealing protocol (each player's encrypted cards travel around the ring, getting one decryption layer stripped at each hop until they return fully decrypted to the owner).

## `/internal/transport`
Provides `GobConn`, a thread-safe wrapper around a `net.Conn` that uses Go's `encoding/gob` for frame serialization. All dispatch and peer connections use this as their transport layer. Exposes `Send`, `Receive`, `Close`, and `RawConn` methods.
