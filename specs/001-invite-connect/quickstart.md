# Quickstart: Invite-Code Node Connection

Validates the feature end-to-end. See [contracts/](contracts/) and
[data-model.md](data-model.md) for details.

## Prerequisites

```bash
go build -o /tmp/music_p2p_node ./cmd/node
```

## Scenario 1 — Invite code shown at startup (FR-007)

```bash
/tmp/music_p2p_node --listen 4001 --api-port 8080
```
**Expected**: startup output prints a clearly labeled invite code line, e.g.
`Your invite code: music:join:eyJ2Ijox...` (and a reachability note if only local
addresses are known).

## Scenario 2 — Fetch invite via API (FR-001/002, US1)

```bash
curl -s http://localhost:8080/api/v1/peers/invite | jq .
```
**Expected**: `data.invite` is a single `music:join:...` string; `data.reachable`
is a boolean; `data.note` explains limited reachability when `reachable=false`.

## Scenario 3 — Join a friend by code (US1, FR-003/004)

Two nodes on the same machine/LAN:
```bash
/tmp/music_p2p_node --listen 4001 --api-port 8080 --no-mdns &   # node A
/tmp/music_p2p_node --listen 4002 --api-port 8081 --no-mdns &   # node B

CODE=$(curl -s http://localhost:8080/api/v1/peers/invite | jq -r .data.invite)
curl -s -X POST http://localhost:8081/api/v1/peers/join \
  -H 'Content-Type: application/json' -d "{\"invite\":\"$CODE\"}" | jq .
```
**Expected**: `success=true`, `data.connected=true`; then
`curl http://localhost:8081/api/v1/peers` lists node A.

## Scenario 4 — Join via CLI flag (US1)

```bash
/tmp/music_p2p_node --listen 4003 --api-port 8082 --no-mdns --join "$CODE"
```
**Expected**: log shows a successful connection to the invited peer at startup.

## Scenario 5 — End-to-end: connect then listen

After Scenario 3, with node A having shared a track (`POST /tracks/share`,
`announce=true`):
```bash
curl -s http://localhost:8081/api/v1/metadata | jq .          # B sees A's track
CID=...                                                        # from metadata
ffplay http://localhost:8081/api/v1/tracks/$CID/stream         # B listens over P2P
```
**Expected**: B discovers and plays A's track — the full deploy→connect→listen flow.

## Scenario 6 — Bad input is rejected, not hung (FR-008/009, US3)

```bash
curl -s -X POST http://localhost:8081/api/v1/peers/join \
  -H 'Content-Type: application/json' -d '{"invite":"garbage"}' | jq .
```
**Expected**: HTTP 400 with `error="malformed invite"`, returned immediately.

Self-invite:
```bash
SELF=$(curl -s http://localhost:8081/api/v1/peers/invite | jq -r .data.invite)
curl -s -X POST http://localhost:8081/api/v1/peers/join -d "{\"invite\":\"$SELF\"}"
```
**Expected**: HTTP 400, `error="cannot join your own node"`.

## Scenario 7 — LAN zero-config still works (US2, FR-011)

```bash
/tmp/music_p2p_node --listen 4001 --api-port 8080 &   # mDNS on (default)
/tmp/music_p2p_node --listen 4002 --api-port 8081 &
sleep 5
curl -s http://localhost:8081/api/v1/peers | jq '.data | length'
```
**Expected**: ≥ 1 peer — nodes auto-connected with no code exchanged.
