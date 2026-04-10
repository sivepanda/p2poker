# User Interface
The client nodes are designed to be fully headless. They exist on a client machine and expose a local port that is available for client User Interface applications to connect to and implement functions for. That way, the client is fully UI agnostic, and anyone can create their very own UI Layer.

## Protocol: gRPC + Protobuf
The local port speaks gRPC over Protocol Buffers. The `.proto` file at `proto/clientrpc/v1/clientrpc.proto` is the canonical contract — frontend authors read this to know the full API surface. Running `protoc` (or `make proto`) against it generates native client stubs in any language (Go, TypeScript, Python, C++, etc.), so a React web app, a terminal TUI, and a native C client can all connect to the same running node without any adapter code.

The gRPC server is a zero-logic passthrough. Every RPC maps one-to-one to a `peer.Node` method — `CreateSession`, `JoinSession`, `ListPeers`, `ConnectPeers`, and `GetNodeInfo` are simple request/response calls. `SubscribeEvents` is a server-side stream: the frontend opens it once and receives a push whenever a P2P message arrives at the node, so the UI can react to game state changes in real time without polling.

## Starting the RPC Layer
A node exposes the gRPC port when started with the `--rpc-addr` flag:

```sh
go run ./cmd/node -dispatch 127.0.0.1:9000 -create -session table-1 -rpc-addr :50051
```

With no `--rpc-addr`, the node behaves exactly as before — headless with no frontend attachment point. When set, a frontend can connect to `localhost:50051` and drive the node entirely through the generated stubs.

## Architecture
The frontend is a thin rendering and input layer with no game logic. All session management, cryptography, and P2P networking remain inside the Go node. The RPC boundary enforces this separation: the frontend can ask the node to create a session or list peers, but it cannot craft raw P2P messages or touch the mesh directly. When the game engine is built, game-specific RPCs (fold, raise, deal) will be added to the same `.proto` file, and the frontend will call those — the node translates them into the appropriate P2P protocol messages internally.