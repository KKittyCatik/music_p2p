# music_p2p

A fully decentralised P2P music streaming node written in Go. Nodes discover each other via a Kademlia DHT, download MP3 tracks in parallel from multiple peers, and play them back in real time with adaptive bitrate selection.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                        music_p2p node                                │
│                                                                      │
│  ┌─────────────┐    ┌──────────────┐    ┌──────────────────────────┐│
│  │   cmd/node  │───▶│  p2p / host  │◀──▶│  Other peers (libp2p)   ││
│  │  (main.go)  │    │  /music/1.0.0│    └──────────────────────────┘│
│  └──────┬──────┘    └──────┬───────┘                                │
│         │                  │                                         │
│         │          ┌───────▼────────┐    ┌───────────────────────┐  │
│         │          │   dht / KadDHT │◀──▶│  IPFS bootstrap peers │  │
│         │          │  (Provide,     │    └───────────────────────┘  │
│         │          │  FindProviders)│                                │
│         │          └───────┬────────┘                                │
│         │                  │                                         │
│  ┌──────▼──────────────────▼───────────────────────────────────────┐│
│  │                    streaming / Engine                            ││
│  │   Scheduler ──▶ ChunkRequests ──▶ goroutines ──▶ peer streams   ││
│  │   (priority, rarity, max_inflight=8, window=50)                  ││
│  └──────────────────────────┬───────────────────────────────────────┘│
│                             │                                        │
│  ┌──────────────┐   ┌───────▼────────┐   ┌──────────────────────┐  │
│  │  storage     │   │  scoring       │   │  bitrate / Adaptive  │  │
│  │  (SHA-256    │   │  (latency,     │   │  (EMA bandwidth,     │  │
│  │   CID, MP3   │   │   throughput,  │   │   20% headroom)      │  │
│  │   frame split│   │   success rate)│   └──────────────────────┘  │
│  └──────────────┘   └────────────────┘                              │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  metadata / Store  (gossipsub "music-metadata" topic)        │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  audio / Player  (beep + mp3 decoder → system speaker)       │   │
│  └──────────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────────┘
```

### Component overview

| Package | Responsibility |
|---------|---------------|
| `cmd/node` | CLI entry point; wires all components together |
| `internal/p2p` | libp2p host creation, stream protocol `/music/1.0.0` |
| `internal/dht` | Kademlia DHT (server mode), Provide / FindProviders |
| `internal/storage` | MP3 frame-aligned chunking, in-memory chunk store |
| `internal/streaming` | Parallel chunk download engine, `io.Reader` interface |
| `internal/scheduler` | Priority + rarity-based chunk scheduler (window=50) |
| `internal/scoring` | Per-peer scoring: success rate + throughput + latency |
| `internal/audio` | Beep/MP3 decoder → system audio playback |
| `internal/bitrate` | Adaptive bitrate variant selection with EMA bandwidth |
| `internal/metadata` | GossipSub metadata distribution and local search |

---

## Build

```bash
# Install ALSA development headers (Linux)
sudo apt-get install -y libasound2-dev

# Fetch dependencies
GOPROXY="https://proxy.golang.org,direct" go mod tidy

# Build
go build ./...

# Compile the node binary
go build -o music_p2p_node ./cmd/node
```

---

## Usage

### Share a local MP3 file

```bash
# Start a node that shares a track and announces it to the DHT
./music_p2p_node --listen 4001 --share /path/to/song.mp3 --announce
```

The node prints the **CID** (SHA-256 hex) and keeps running to serve chunks to peers.

### Play a track

```bash
# Connect to a sharing peer and stream a track by CID
./music_p2p_node --listen 4002 \
  --connect /ip4/1.2.3.4/tcp/4001/p2p/<PEER_ID> \
  --play <CID>
```

### Search for tracks

```bash
# Search the local metadata store (populated via gossipsub)
./music_p2p_node --listen 4002 --search "Pink Floyd"
```

### Run a relay/bootstrap node

```bash
# Keep a node running without playing anything
./music_p2p_node --listen 4001
```

---

## CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--listen <port>` | `4001` | TCP port the node listens on |
| `--connect <addr>` | — | Peer multiaddr to dial on startup (e.g. `/ip4/1.2.3.4/tcp/4001/p2p/<ID>`) |
| `--play <cid>` | — | Hex CID of the track to stream and play |
| `--search <query>` | — | Case-insensitive search on Title / Artist in local metadata |
| `--share <path>` | — | Path to a local MP3 file to load into storage |
| `--announce` | `false` | Announce shared tracks to DHT and publish metadata via gossipsub |

---

## P2P protocol

### Stream protocol `/music/1.0.0`

All chunk exchange uses a simple line-based request/response protocol over a libp2p stream:

```
Client → Server:  GET <cid> <index>\n
Server → Client:  <raw chunk bytes>   (or "ERR\n" on failure)

Client → Server:  TOTAL <cid>\n
Server → Client:  <n>\n              (total number of chunks)
```

### Content addressing

Tracks are identified by the **hex-encoded SHA-256** hash of the raw MP3 file.  Chunks are split on MP3 sync-word boundaries (0xFF 0xEx/0xFx), grouping 32 frames per chunk.

### DHT content routing

Providers announce content via `IpfsDHT.Provide()` using a `CIDv1(Raw, SHA2-256)` derived from the track's hex CID.  Consumers call `FindProviders()` to discover peers before streaming.

### Metadata gossip

Track metadata (title, artist, duration, bitrate variants) is published as JSON on the `music-metadata` gossipsub topic and replicated to all connected peers automatically.

### Adaptive bitrate

The engine tracks bandwidth with an exponential moving average (α = 0.3) and selects the highest quality variant whose bitrate satisfies `bandwidth ≥ bitrate × 1.2`.

---

## Technical details

- **Go version**: 1.24
- **libp2p**: v0.39.1
- **kad-dht**: v0.28.2
- **pubsub**: v0.15.0
- **beep (audio)**: v1.4.1
- **go-cid**: v0.5.0