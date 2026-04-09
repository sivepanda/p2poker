# Running

## Prerequisites

- Go 1.26+

## Build

```sh
make build   # produces ./app (sim), ./dispatch, ./node binaries
make clean   # removes built binaries
```

## Simulation (Local Dealing Demo)

Runs a local-only simulation of the SRA shuffle and deal protocol with 4 nodes. No networking involved — nodes communicate via in-memory channels.

```sh
make sim
# or
go run ./cmd/sim
```

## Dispatch Server + Nodes (P2P Framework)

The dispatch server is a lightweight discovery service that lets nodes find each other. It does not participate in game logic — it only tracks sessions and peer addresses via a lease-based registration model.

### 1. Start the Dispatch Server

```sh
go run ./cmd/dispatch -addr :9000
```

Flags:  

| Flag | Default | Description |
|------|---------|-------------|
| `-addr`| `:9000`| TCP Listen Address |
| `-lease-ttl` | `10s`| How long a node's registration lasts before it must heartbeat again |

### 2. Create a Session (First Node)

```sh
go run ./cmd/node -dispatch 127.0.0.1:9000 -create -session table-1 -list-peers
```

### 3. Join the Session (Additional Nodes)

```sh
go run ./cmd/node -dispatch 127.0.0.1:9000 -session table-1 -list-peers
```

### 4. Send Messages Between Nodes

Send a direct message to a specific peer:
```sh
go run ./cmd/node -dispatch 127.0.0.1:9000 -session table-1 -send-to node-2 -body "hello from node-1"
```

Broadcast to all peers in the session:
```sh
go run ./cmd/node -dispatch 127.0.0.1:9000 -session table-1 -broadcast -body "hello everyone"
```

### Node Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-dispatch` | `127.0.0.1:9000` | Dispatch server address |
| `-peer-addr` | `:0` | Local TCP address for peer-to-peer connections (`:0` picks a random port) |
| `-id` | *(auto-assigned)* | Optional node ID |
| `-create` | `false` | Create a new session instead of joining |
| `-session` | | Session ID to create or join |
| `-list-peers` | `false` | Print the peer list after joining |
| `-send-to` | | Target node ID for a direct message |
| `-broadcast` | `false` | Broadcast message to all session peers |
| `-body` | `"hello"` | Message body |
