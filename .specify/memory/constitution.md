<!--
SYNC IMPACT REPORT
==================
Version change: 1.1.0 → 1.2.0  (MINOR: principles VII, VIII, IX added)
Modified principles: none
Added sections:
  - Principle VII. Security-by-Default
  - Principle VIII. Audio Quality SLA
  - Principle IX. Simplicity / No Premature Abstraction
Removed sections: none
Templates requiring updates:
  - .specify/templates/plan-template.md  ✅ updated — gates VII, VIII, IX added to Constitution Check table
  - .specify/templates/tasks-template.md ✅ aligned — security hardening and polish tasks already in final phase
Follow-up TODOs: none
-->

# music_p2p Constitution

## Core Principles

### I. P2P-First

Every feature MUST work in a fully decentralized, multi-node environment with no single point of failure.
Content routing MUST use the Kademlia DHT (`Provide` / `FindProviders`).
No centralized coordination server is permitted — nodes discover each other via mDNS (LAN) and DHT rendezvous.
Features that require a central coordinator are out of scope.

**Rationale**: The entire value proposition of music_p2p is censorship-resistant, serverless music sharing.
Any centralized dependency negates that guarantee.

### II. Content Integrity by Default

All content is identified by its SHA-256 hash (CID = `hex(SHA-256(rawFile))`).
Data MUST be verified on receipt; tampered or incomplete chunks MUST be rejected silently.
Track metadata MUST carry a libp2p private-key signature; unsigned or unverifiable messages MUST be dropped.
The `MetaID = hex(SHA-256(title + artist + duration))` deduplication identity MUST be preserved.

**Rationale**: In an open P2P network any peer can inject bad data.
Cryptographic addressing and signing are the only reliable defences.

### III. Resilience & Graceful Degradation

Nodes MUST survive peer disconnection, network stall, and partial failures without panicking or hanging.
The anti-stall monitor (≤ 2 s without a chunk → panic mode), backpressure (`MAX_BUFFER = 50` chunks), and
congestion-control window (2–32 in-flight requests) are NON-NEGOTIABLE and MUST NOT be removed or disabled.
Every long-running goroutine MUST respect `context.Context` cancellation and clean up on `Close()`.

**Rationale**: A music node that freezes on a slow peer is unusable.
Resilience mechanisms directly determine perceived audio quality.

### IV. Mock-Isolated Tests

All unit and integration tests MUST use in-memory mocks.
No real libp2p host, no real DHT, no real network connections are permitted in `go test ./...`.
REST API tests MUST use `httptest.NewRecorder`; streaming tests MUST use in-memory `storage.Storage`.
Tests MUST be runnable offline with `go test ./...` and complete in under 30 s on a developer machine.

**Rationale**: Network-dependent tests are flaky, slow, and block CI.
The existing mock-based suite proved that 75+ tests can give strong confidence without any real network.

### V. Observability as a First-Class Concern

Every critical path (chunk download, peer connection, playback start/stop, DHT operations) MUST emit
structured log events via `go.uber.org/zap` and increment the relevant Prometheus counter/gauge.
The optional Grafana + Loki + Prometheus profile (`make up-observability`) MUST remain functional and
in sync with any new metrics added.
Silent failures (no log, no metric) are prohibited.

**Rationale**: Distributed systems fail in subtle ways.
Without metrics and logs there is no way to diagnose real-world multi-node playback issues.

### VI. Test-Driven Development (NON-NEGOTIABLE)

Tests MUST be written and confirmed failing **before** any production code is written (Red → Green → Refactor).
The cycle is strict: write the smallest failing test → make it pass with minimal code → refactor.
No new package, handler, or exported function is permitted to ship without a corresponding test.
Skipping or deferring tests to "later" is prohibited; `t.Skip()` requires a linked issue in the skip message.

**Rationale**: The existing 75-test suite was built test-first and caught several integration-level issues
before they reached production. Relaxing TDD would degrade confidence in a codebase that has no staging
environment — the test suite is the only safety net.

### VII. Security-by-Default

All inputs arriving from the network or the REST API are untrusted and MUST be sanitised before use:

- **File paths**: MUST be passed through `filepath.Clean` and verified to remain within `baseDir`; directory traversal (`../`) MUST be rejected.
- **CIDs**: MUST be validated as lowercase hex strings before any storage or DHT lookup; arbitrary strings MUST NOT be used as map keys or file names.
- **Multiaddrs**: MUST be parsed through `ma.NewMultiaddr` and `peer.AddrInfoFromP2pAddr`; raw strings MUST NOT be dialled.
- **Shell commands**: No shell command MAY be constructed from peer-supplied or user-supplied data.
- **Gossipsub messages**: MUST be signature-verified before being stored (already enforced by Principle II); any message failing verification MUST be dropped and logged.

**Rationale**: The REST API is publicly accessible and peers are anonymous.
A single unsanitised path or unvalidated CID can compromise the host filesystem or enable denial-of-service.

### VIII. Audio Quality SLA

The following guarantees are NON-NEGOTIABLE for the playback experience:

- **Startup latency**: audio playback MUST begin within 500 ms of `StartStreaming` being called (`WaitForChunks` ≤ `InstantPlaybackChunks`).
- **Gapless transitions**: the next track MUST be pre-loaded before the current track ends; `Player.SetNext` MUST be called before `doneCh` fires.
- **Adaptive bitrate headroom**: the ABR selector MUST maintain a 20 % bandwidth headroom; it MUST NOT select a variant that exceeds the current EMA estimate.
- **Anti-stall threshold**: the stall monitor MUST fire within 2 s of zero chunk progress and MUST trigger ABR downgrade + scheduler window reset.

Any refactoring of `streaming/engine.go`, `audio/player.go`, or `bitrate/adaptive.go` MUST verify these SLAs
via the existing test suite before merging.

**Rationale**: Users tolerate brief buffering but not stuttering mid-song or silence between tracks.
These are the concrete promises music_p2p makes to every listener.

### IX. Simplicity / No Premature Abstraction

- **Interfaces**: introduce an interface only when two or more concrete implementations exist or are imminent in the same PR.
- **New packages**: a new `internal/` package requires a one-line justification in the PR description explaining why an existing package cannot be extended.
- **Dependencies**: prefer packages already in `go.mod`; adding a new `require` entry for a utility replaceable with ≤ 10 lines of stdlib code is prohibited.
- **YAGNI**: do not design for hypothetical future requirements; three similar lines are better than a premature abstraction.
- **Complexity budget**: if a function exceeds ~50 lines or 4 levels of nesting, it MUST be refactored before the PR is merged.

**Rationale**: The streaming engine, scheduler, and connpool are already non-trivial.
Unnecessary abstraction layers make debugging distributed failures significantly harder.

## Technology & API Contract

- **Runtime**: Go 1.24+. No downgrade without a documented migration plan.
- **P2P transport**: `go-libp2p` is the canonical networking layer. Direct raw TCP for P2P is prohibited.
- **Stream protocol**: `/music/1.0.0` line-based text protocol over libp2p streams.
  Breaking changes MUST bump the protocol version (`/music/2.0.0`).
- **REST API**: All endpoints MUST be documented with `swaggo/swag` annotations before merging.
  `docs/swagger.json` and `docs/swagger.yaml` are the source-of-truth API contract.
  Undocumented endpoints MUST NOT be shipped.
- **New dependencies**: Any new `require` entry in `go.mod` requires explicit justification in the PR description.
  Prefer packages already in the dependency tree.

## Development Workflow

- **Branch naming**: `###-short-description` (e.g., `042-gapless-seek-fix`).
- **Specs first**: non-trivial features MUST have a spec in `specs/###-feature-name/spec.md`
  before implementation begins (SDD workflow via `/speckit-specify` → `/speckit-plan` → `/speckit-tasks`).
- **Tests gate merge**: `go test ./...` MUST pass with zero failures. No `--count=0` bypasses.
- **Swagger regeneration**: run `swag init -g cmd/node/main.go` after any handler/model change
  and commit the updated `docs/` files in the same PR.
- **Docker smoke test**: `make up` MUST succeed and the `/api/v1/status` endpoint MUST return 200
  before any PR targeting `main` is merged.

## Governance

This constitution supersedes all other project conventions and individual preferences.
Where a conflict exists, the constitution wins.

**Amendment procedure**:
1. Open a PR with changes to this file.
2. State the version bump type (MAJOR / MINOR / PATCH) and rationale in the PR description.
3. Update `README.md` and any affected template or doc files in the same PR.
4. Merge requires at least one explicit approval from a project maintainer.

**Versioning policy** (semantic):
- MAJOR: removal or redefinition of an existing principle.
- MINOR: new principle or section added.
- PATCH: clarifications, wording, typo fixes.

**Compliance review**: every PR description MUST include a one-line "Constitution Check"
confirming which principles are affected and that none are violated.

**Version**: 1.2.0 | **Ratified**: 2026-06-22 | **Last Amended**: 2026-06-22
