# music_p2p

A fully decentralised P2P music streaming node written in Go. Nodes discover each other via a Kademlia DHT, download MP3 tracks in parallel from multiple peers, play them back with gapless audio and adaptive bitrate selection, and expose a complete REST API with Swagger documentation.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          music_p2p node                                  │
│                                                                          │
│  ┌─────────────┐   ┌─────────────────────────────────────────────────┐  │
│  │  cmd/node   │──▶│  REST API  (gorilla/mux + Swagger UI /swagger/) │  │
│  │  (main.go)  │   │  internal/api  — handlers, models, middleware   │  │
│  └──────┬──────┘   └─────────────────────────────────────────────────┘  │
│         │                                                                │
│         │          ┌──────────────┐    ┌──────────────────────────────┐ │
│         └─────────▶│  p2p / host  │◀──▶│  Other peers (libp2p)        │ │
│                    │  /music/1.0.0│    └──────────────────────────────┘ │
│                    └──────┬───────┘                                      │
│                           │                                              │
│                   ┌───────▼────────┐    ┌───────────────────────┐       │
│                   │   dht / KadDHT │◀──▶│  IPFS bootstrap peers │       │
│                   │  (Provide,     │    └───────────────────────┘       │
│                   │  FindProviders)│                                     │
│                   └───────┬────────┘                                     │
│                           │                                              │
│  ┌────────────────────────▼─────────────────────────────────────────┐   │
│  │                   streaming / Engine                              │   │
│  │  Scheduler ──▶ ChunkRequests ──▶ goroutines ──▶ connpool streams │   │
│  │  (Critical/High/Prefetch zones, congestion control, anti-stall)  │   │
│  └─────────┬───────────────────────────────────────────┬────────────┘   │
│            │                                           │                │
│  ┌─────────▼──────┐  ┌──────────────┐  ┌─────────────▼────────────┐   │
│  │  connpool/Pool │  │  scoring     │  │  bitrate / Adaptive      │   │
│  │  (stream reuse │  │  (latency,   │  │  (EMA bandwidth,         │   │
│  │   30s TTL,     │  │   throughput,│  │   20% headroom,          │   │
│  │   max 8/peer)  │  │   success %) │  │   variant selection)     │   │
│  └────────────────┘  └──────────────┘  └──────────────────────────┘   │
│                                                                          │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │  metadata / Store  (signed gossipsub "music-metadata" topic)     │   │
│  │  MetaID dedup · SHA-256 signatures · local search               │   │
│  └──────────────────────────────────────────────────────────────────┘   │
│                                                                          │
│  ┌──────────────┐  ┌────────────────────────────────────────────────┐   │
│  │  storage     │  │  audio / Player  (gapless dual-stream beep)    │   │
│  │  (SHA-256    │  │  queue / Queue   (history, insert, autoplay)   │   │
│  │   CID, MP3   │  └────────────────────────────────────────────────┘   │
│  │   frame split│                                                        │
│  └──────────────┘                                                        │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## Component overview

| Package | Responsibility |
|---------|---------------|
| `cmd/node` | CLI entry point; wires all components together |
| `internal/api` | REST HTTP API server, 20+ endpoint handlers, middleware, JSON models |
| `internal/p2p` | libp2p host creation, stream protocol `/music/1.0.0` |
| `internal/dht` | Kademlia DHT (server mode), `Provide` / `FindProviders` |
| `internal/storage` | MP3 frame-aligned chunking, in-memory chunk store, `RemoveTrack` |
| `internal/streaming` | Parallel chunk download engine, backpressure, seek, anti-stall, `io.Reader` |
| `internal/scheduler` | Priority zones (Critical/High/Prefetch), rarity-based dispatch, congestion control |
| `internal/scoring` | Per-peer scoring: success rate + throughput + latency |
| `internal/connpool` | Per-peer libp2p stream pool, 30 s TTL, max 8 streams/peer, least-loaded selection |
| `internal/bitrate` | Adaptive bitrate variant selection with EMA bandwidth estimate |
| `internal/metadata` | Signed gossipsub metadata, MetaID dedup, local search |
| `internal/queue` | Smart playback queue: history, `Insert`, `Peek`, `Current`, autoplay |
| `internal/audio` | Gapless dual-stream beep/MP3 playback → system speaker |
| `docs` | Generated Swagger/OpenAPI spec (`swagger.json`, `swagger.yaml`) |

---

## Build

```bash
# Install ALSA development headers (Linux only)
sudo apt-get install -y libasound2-dev

# Fetch dependencies
GOPROXY="https://proxy.golang.org,direct" go mod tidy

# Build all packages
go build ./...

# Compile the node binary
go build -o music_p2p_node ./cmd/node

# Run all tests
go test ./...
```

---

## Docker deployment

### Default: single node

`docker compose up` starts **only one** `music_p2p` node with ports:

| Port (host→container) | Purpose |
|-----------------------|---------|
| `4001:4001` | P2P / libp2p |
| `8080:8080` | REST API + Swagger |
| `9090:9090` | Prometheus metrics |

```bash
# Build and start a single node
make up
# or
docker compose up -d --build
```

The Prometheus / Grafana / Loki stack is **not started** by default.

### Observability profile (optional)

To also start Prometheus, Grafana and Loki:

```bash
make up-observability
# or
docker compose --profile observability up -d --build
```

| Service | URL |
|---------|-----|
| Grafana | http://localhost:3000 (admin / admin) |
| Prometheus | http://localhost:9092 |
| Loki | http://localhost:3100 |

### Joining nodes across machines

Each machine runs its own `docker compose up`. Nodes find each other through:

1. **mDNS** – automatic discovery on the same LAN (no configuration needed).
2. **Invite codes** (easiest across the internet, no servers) – each node prints a
   one-line **invite code** at startup (and serves it at `GET /api/v1/peers/invite`).
   Send the code to a friend; they connect with it — no IPs, ports, or peer IDs to
   type. NAT traversal is automatic via UPnP/NAT-PMP port mapping and AutoNAT.

   ```bash
   # Node A — copy the code from startup logs or:
   curl -s http://localhost:8080/api/v1/peers/invite | jq -r .data.invite
   #   → music:join:eyJ2Ijox...

   # Node B — join with the code (or pass --join "<code>" at startup):
   curl -X POST http://localhost:8080/api/v1/peers/join \
     -H 'Content-Type: application/json' -d '{"invite":"music:join:eyJ2Ijox..."}'
   ```

   If a node reports `"reachable": false`, only local addresses are known — remote
   peers may not connect until UPnP is enabled on the router or TCP port 4001 is
   forwarded manually.
3. **Bootstrap** – for advanced setups, pass at least one known peer address via the `BOOTSTRAP_ADDRS` environment variable (comma-separated multiaddrs):

```bash
# Machine A – start first, note the peer ID printed in logs
docker compose up -d

# Machine B – bootstrap to Machine A
BOOTSTRAP_ADDRS=/ip4/<IP_A>/tcp/4001/p2p/<PEER_ID_A> docker compose up -d

# Machine C – any already-running node works as bootstrap
BOOTSTRAP_ADDRS=/ip4/<IP_B>/tcp/4001/p2p/<PEER_ID_B> docker compose up -d
```

After the first connection, Kademlia DHT rendezvous (`music-p2p-network` namespace) propagates peer discovery automatically — subsequent nodes only need one bootstrap address.

To disable mDNS (e.g. in cloud environments where multicast is unavailable):

```bash
NO_MDNS=1 BOOTSTRAP_ADDRS=... docker compose up -d
```

### Make targets

| Target | Description |
|--------|-------------|
| `make up` | Start single `node` container |
| `make up-observability` | Start node + Prometheus + Grafana + Loki |
| `make down` | Stop and remove all containers and volumes |
| `make logs` | Follow logs of the node container |
| `make logs-observability` | Follow logs of all containers (with observability profile) |

---

## Usage

### Share a local MP3 and announce it

```bash
./music_p2p_node --listen 4001 --share /path/to/song.mp3 --announce
```

The node prints the **CID** (SHA-256 hex) and keeps running to serve chunks to peers.

### Stream and play a track

```bash
./music_p2p_node --listen 4002 \
  --connect /ip4/1.2.3.4/tcp/4001/p2p/<PEER_ID> \
  --play <CID>
```

### Autoplay a queue of tracks

```bash
./music_p2p_node --listen 4002 \
  --connect /ip4/1.2.3.4/tcp/4001/p2p/<PEER_ID> \
  --queue <CID1>,<CID2>,<CID3>
```

### Search for tracks

```bash
./music_p2p_node --listen 4002 --search "Pink Floyd"
```

### Start with the REST API enabled

```bash
./music_p2p_node --listen 4001 --api-port 8080
```

Navigate to `http://localhost:8080/swagger/` for the interactive Swagger UI.

### Run a relay/bootstrap node

```bash
./music_p2p_node --listen 4001
```

---

## CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--listen <port>` | `4001` | TCP port the node listens on |
| `--connect <addr>` | — | Peer multiaddr to dial on startup (e.g. `/ip4/1.2.3.4/tcp/4001/p2p/<ID>`) |
| `--play <cid>` | — | Hex CID of the track to stream and play |
| `--search <query>` | — | Case-insensitive search on Title/Artist in the local metadata store |
| `--share <path>` | — | Path to a local MP3 file to load into storage |
| `--announce` | `false` | Announce shared tracks to DHT and publish metadata via gossipsub |
| `--queue <cids>` | — | Comma-separated list of CIDs for autoplay queue |
| `--api-port <port>` | `0` (disabled) | Port for the REST API + Swagger server |

---

## REST API reference

All endpoints are prefixed with `/api/v1`. Responses are JSON with the shape `{"success": bool, "data": ..., "error": "..."}`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/status` | Node status (ID, uptime, peer count) |
| `GET` | `/tracks` | List all locally stored tracks |
| `POST` | `/tracks/share` | Upload & share an MP3 (multipart/form-data: `file`, optional `announce=true`) |
| `GET` | `/tracks/{cid}/stream` | **Stream the track's MP3 audio** (works for local & P2P tracks; point a browser/VLC at it to listen) |
| `DELETE` | `/tracks/{cid}` | Remove a track from local storage |
| `GET` | `/metadata` | All metadata entries |
| `GET` | `/metadata/search?q=` | Search by title/artist (case-insensitive) |
| `GET` | `/metadata/{cid}` | Get metadata for a specific CID |
| `POST` | `/metadata` | Publish metadata to gossipsub |
| `POST` | `/playback/play` | Set the current-track state (body: `{"cid": "..."}`) — **state only, no audio**; to listen use `/tracks/{cid}/stream` |
| `POST` | `/playback/stop` | Clear the current-track state |
| `POST` | `/playback/seek` | Update the current-track position marker (body: `{"chunk_index": N}`) |
| `GET` | `/playback/status` | Current playback state (CID, chunk, playing) |
| `GET` | `/queue` | Current queue state (upcoming + current item) |
| `POST` | `/queue` | Enqueue a track (body: `{"cid": "...", "title": "...", "artist": "..."}`) |
| `POST` | `/queue/insert` | Insert a track at a position (body adds `"position": N`) |
| `DELETE` | `/queue` | Clear the upcoming queue |
| `GET` | `/queue/history` | Play history (oldest first) |
| `GET` | `/peers` | Connected peers with scoring data |
| `POST` | `/peers/connect` | Connect to a peer (body: `{"multiaddr": "/ip4/..."}`) |
| `GET` | `/peers/invite` | **Get this node's shareable invite code** (+ reachability) |
| `POST` | `/peers/join` | **Connect using an invite code** (body: `{"invite": "music:join:..."}`) |
| `GET` | `/peers/{peerID}/score` | Scoring details for a specific peer |
| `POST` | `/dht/provide/{cid}` | Announce a CID to the DHT |
| `GET` | `/dht/providers/{cid}` | Find providers for a CID |
| `GET` | `/engine/status` | Streaming engine stats (chunks buffered, ABR bitrate) |
| `GET` | `/swagger/*` | Interactive Swagger UI |

> **Playback endpoints are a control plane, not an audio sink.** A node is
> typically headless (Docker, no speaker), so `/playback/play`, `/stop`, `/seek`
> only read and write the node's in-memory "current track" state — they do **not**
> start a P2P download or emit audio. (An earlier version called the streaming
> engine here with nothing consuming it; the buffer filled, the anti-stall
> monitor logged *"stall detected"* on a loop, and libp2p streams to the peer
> leaked.) **To actually hear audio, stream it over HTTP from
> `GET /tracks/{cid}/stream`** (see the quickstart below), which drives its own
> engine and is read to completion.

---

## Quickstart: share on one node, listen on another (over HTTP)

No audio hardware required on either node — you listen by pointing any HTTP media
player (browser `<audio>`, VLC, `ffplay`, `mpv`) at the streaming endpoint.

```bash
# Node A — share a track and announce it to the network
curl -X POST http://NODE_A:8080/api/v1/tracks/share \
  -F "file=@song.mp3" -F "announce=true"
# → {"success":true,"data":{"cid":"<CID>","chunk_count":426}}

# Node B — connect to A (or rely on mDNS/bootstrap), then discover what's shared
curl -X POST http://NODE_B:8080/api/v1/peers/connect \
  -H 'Content-Type: application/json' \
  -d '{"multiaddr":"/ip4/<IP_A>/tcp/4001/p2p/<PEER_ID_A>"}'

curl http://NODE_B:8080/api/v1/metadata          # browse tracks gossiped from peers
# → find the <CID> you want

# Node B — listen: stream the track over P2P straight into your player
ffplay  http://NODE_B:8080/api/v1/tracks/<CID>/stream
# or open the same URL in a browser, or:  curl ... -o song.mp3
```

The streaming endpoint serves locally-stored tracks directly and fetches remote
tracks from peers on demand, returning a continuous `audio/mpeg` byte stream.

---

## Swagger

When `--api-port` is set, the Swagger UI is available at:

```
http://localhost:<api-port>/swagger/
```

The raw OpenAPI spec is at `docs/swagger.json` and `docs/swagger.yaml`.

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

Tracks are identified by the **hex-encoded SHA-256** hash of the raw MP3 file. Chunks are split on MP3 sync-word boundaries (`0xFF 0xEx/0xFx`), grouping 32 frames per chunk.

### DHT content routing

Providers announce content via `IpfsDHT.Provide()` using a `CIDv1(Raw, SHA2-256)` derived from the track's hex CID. Consumers call `FindProviders()` to discover peers before streaming.

### Metadata gossip

Track metadata (title, artist, duration, bitrate variants) is published as a **signed JSON envelope** on the `music-metadata` gossipsub topic. Every message carries the publisher's peer ID and a libp2p private-key signature. Receivers verify the signature and reject any unsigned or tampered messages.

---

## Advanced features

| Feature | Description |
|---------|-------------|
| **Gapless playback** | `audio.Player` maintains a dual-stream `gaplessStreamer`; the next track is pre-loaded so transitions are seamless. |
| **Instant playback** | `Engine.WaitForChunks()` gates audio start until a minimal initial buffer is ready (<= 0.5 s). |
| **Congestion control** | Scheduler dynamically adjusts `maxInflight` (2--32): halved on 3 consecutive failures, ramped up on >= 90% success rate. |
| **Backpressure** | `MAX_BUFFER = 50` chunks; the download loop blocks via `readCond.Wait()` when the buffer is full and resumes when the consumer catches up. |
| **Smart prefetch** | Three priority zones: *Critical* (chunks 0–5 ahead), *High* (5–20), *Prefetch* (20+). Critical chunks bypass the inflight limit. |
| **Seek consistency** | `Engine.Seek(chunkIndex)` atomically clears the buffer, resets the read pointer, and wakes the consumer — no stale data leaks across seeks. |
| **Peer connection pool** | `connpool.Pool` reuses open libp2p streams per peer (30 s TTL, max 8 concurrent, least-loaded selection). Background reaper stops cleanly via `Close()`. |
| **Anti-stall** | A separate goroutine monitors time-since-last-chunk every 500 ms. After 2 s with no progress it enters *panic mode*: resets the scheduler window and downgrades the ABR estimate. Runs independently of the download loop so it fires even under backpressure. |
| **MetaID dedup** | `MetaID = hex(SHA-256(title + artist + duration))` is a stable content identifier. The store keeps the most-complete entry when duplicates arrive. |
| **Signed metadata** | Metadata is signed with the publisher's libp2p private key before gossipsub broadcast. Receivers extract the public key from the peer ID and verify before storing. Unsigned messages are rejected. |
| **Smart queue** | `queue.Queue` supports ordered enqueue, insert-at-position, peek, current-item, and full play history. Autoplay advances automatically in `main.go`. |
| **Performance** | `sync.Pool` for `bytes.Buffer` (in `ChunkBytes`) and read byte slices (in `fetchChunk`) eliminates per-chunk allocations in hot paths. |

---

## Testing

Run the full test suite:

```bash
go test ./...
```

| Package | Tests | Coverage areas |
|---------|-------|---------------|
| `internal/api` | 13 | All REST endpoints, middleware, error responses |
| `internal/bitrate` | 9 | EMA bandwidth, variant selection, headroom |
| `internal/connpool` | 3 | Acquire/Release, TTL reap, least-loaded |
| `internal/metadata` | 10 | MetaID computation, dedup, search, local store |
| `internal/queue` | 12 | Enqueue, insert, next, peek, history, clear |
| `internal/scheduler` | 10 | Priority zones, inflight limits, congestion |
| `internal/scoring` | 8 | Latency/throughput scoring, decay |
| `internal/storage` | 7 | Chunk split, load/get, remove |
| `internal/streaming` | 3 | Engine start, read, seek |

Tests use `httptest.NewRecorder` for API tests and in-memory mocks for all P2P components — no real network required.

---

## Technical details

| Component | Version |
|-----------|---------|
| Go | 1.24 |
| go-libp2p | v0.39.1 |
| go-libp2p-kad-dht | v0.28.2 |
| go-libp2p-pubsub | v0.15.0 |
| gopxl/beep (audio) | v1.4.1 |
| gorilla/mux | v1.8.1 |
| swaggo/swag | v1.16.4 |
| swaggo/http-swagger | v1.3.4 |
| stretchr/testify | v1.10.0 |
