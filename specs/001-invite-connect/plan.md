# Implementation Plan: Invite-Code Node Connection

**Branch**: `001-invite-connect` | **Date**: 2026-06-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/001-invite-connect/spec.md`

## Summary

Give users a one-string, copy-paste way to connect nodes across machines with no
project-operated servers. A node prints its own **invite code** at startup and
exposes it via the API; a peer joins by submitting that code. Reachability across
home networks is achieved by enabling automatic router port mapping (UPnP/NAT-PMP),
AutoNAT address discovery, and direct hole punching on the libp2p host — no
bootstrap/relay infrastructure. Existing mDNS (LAN) and DHT rendezvous discovery
continue to work unchanged.

## Technical Context

**Language/Version**: Go 1.24

**Primary Dependencies**: go-libp2p v0.39.1 (host, AutoNAT, NATManager/NATPortMap,
holepunch/DCUtR), go-multiaddr; gorilla/mux (REST). All already in `go.mod` — no
new dependencies.

**Storage**: N/A (invite codes are computed from live host state; nothing persisted)

**Testing**: `go test ./...` with in-memory/black-box tests; encode/decode is pure
and unit-testable without a network; API handlers via `httptest`.

**Target Platform**: Linux/macOS node binary + Docker container

**Project Type**: Single Go project (CLI + REST node)

**Performance Goals**: Invite encode/decode < 1 ms; join attempt bounded by a
connect timeout (≤ 30 s per SC-004).

**Constraints**: No always-on servers/relay operated by the project. Must not break
mDNS or DHT rendezvous. Invite must be a single copy-paste-safe token.

**Scale/Scope**: Small — one new internal package, host config changes, two REST
endpoints, one CLI flag, startup output.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Gate question | Result |
|---|-----------|---------------|--------|
| I | P2P-First | Does this feature require a centralized coordinator? | ✅ No — no servers/relay; invite is self-contained, discovery stays mDNS+DHT. |
| II | Content Integrity | Handles chunks/metadata? Integrity preserved? | N/A — connection layer only; does not touch chunk/metadata paths. |
| III | Resilience | New goroutines honour ctx + Close()? | ✅ Join uses a bounded context; NAT services are managed by the libp2p host lifecycle (closed via host.Close). No new long-lived goroutines of our own. |
| IV | Mock-Isolated Tests | All tests run offline with `go test ./...`? | ✅ Invite encode/decode is pure; API tests use httptest; no real network. |
| V | Observability | Each new critical path logs + Prometheus metric? | ✅ Join success/failure logged via zap; add a `peers_joined_total` counter and reachability gauge. |
| VI | TDD | Tests written before implementation? | ✅ Tasks order encode/decode + handler tests before implementation. |
| VII | Security | Network/API inputs sanitised? | ✅ Invite decode strictly validates version + multiaddrs (ma.NewMultiaddr); reject self/malformed; no shell from input. |
| VIII | Audio SLA | Affects engine/player/bitrate? | N/A — no streaming-path changes. |
| IX | Simplicity | New interface/package/dependency justified? | ✅ One small `internal/invite` package (cohesive, testable); zero new deps; no new interfaces. |
| API | API Contract | New endpoints annotated with swaggo + swagger regen? | ✅ `/peers/invite` and `/peers/join` get swaggo annotations; regenerate docs/. |

**Result**: PASS. No violations; Complexity Tracking not required.

## Project Structure

### Documentation (this feature)

```text
specs/001-invite-connect/
├── plan.md              # This file
├── spec.md              # Feature spec
├── research.md          # Phase 0 — decisions on NAT traversal + invite format
├── data-model.md        # Phase 1 — Invite entity & validation rules
├── quickstart.md        # Phase 1 — two-node validation walkthrough
├── contracts/
│   ├── rest-peers.md    # GET /peers/invite, POST /peers/join contracts
│   └── invite-package.md# internal/invite Encode/Decode contract
└── checklists/
    └── requirements.md  # Spec quality checklist (done)
```

### Source Code (repository root)

```text
internal/
├── invite/
│   ├── invite.go        # Encode(host) / Decode(code) + address filtering, version tag
│   └── invite_test.go   # round-trip, loopback filter, malformed/version/self rejection
├── p2p/
│   └── host.go          # StartNode: + NATPortMap, AutoNAT (EnableNATService),
│                        #   EnableHolePunching; keep ListenAddrStrings
├── api/
│   ├── server.go        # routes: GET /peers/invite, POST /peers/join; Config wiring
│   ├── handlers.go      # handleGetInvite, handleJoin
│   ├── models.go        # JoinRequest, InviteResponse
│   └── handlers_test.go # invite/join handler tests (httptest)
├── metrics/
│   └── metrics.go       # PeersJoined counter, Reachability gauge
cmd/node/
└── main.go              # --join flag; print own invite code at startup;
                         #   wire invite factory into api.Config
docs/                    # regenerated swagger
README.md                # invite-code connection section
```

**Structure Decision**: Single Go project. The connection concern is isolated in a
new cohesive `internal/invite` package; host NAT config stays in `internal/p2p`;
the REST surface follows the existing handler/model/route pattern in `internal/api`.

## Complexity Tracking

> No Constitution Check violations — section intentionally empty.
