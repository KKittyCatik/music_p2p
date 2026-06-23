---
description: "Task list for invite-code node connection"
---

# Tasks: Invite-Code Node Connection

**Input**: Design documents from `/specs/001-invite-connect/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: INCLUDED — the constitution mandates TDD (Principle VI) and mock-isolated
tests (Principle IV). Test tasks come before their implementation.

**Organization**: Grouped by user story for independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1, US2, US3 (Setup/Foundational/Polish have no story label)

## Path Conventions

Single Go project: packages under `internal/`, entry point `cmd/node/main.go`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Prepare the package skeleton for the feature.

- [X] T001 Create `internal/invite/` package directory with a `package invite` stub in internal/invite/invite.go (constants `Version=1`, `Prefix="music:join:"`, `Info` struct) so dependent files compile.
- [X] T002 Verify build baseline: `go build ./...` and `go test ./...` pass before changes.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Host reachability + observability that the user stories rely on.

**⚠️ CRITICAL**: Complete before user-story phases.

- [X] T003 Enable NAT traversal in internal/p2p/host.go `StartNode`: add `libp2p.NATPortMap()`, `libp2p.EnableNATService()` (AutoNAT), and `libp2p.EnableHolePunching()` while keeping `ListenAddrStrings`. Keep the signature unchanged.
- [X] T004 [P] Add metrics in internal/metrics/metrics.go: `PeersJoined` counter (label `result`) and `Reachability` gauge; register both in `init()`.
- [X] T005 Manual/dev check: `go build ./...` and start a node, confirm it still boots and mDNS/DHT logs appear (no regression to existing discovery).

**Checkpoint**: Host advertises reachable addrs; metrics registered.

---

## Phase 3: User Story 1 - Share an invite and connect a friend (Priority: P1) 🎯 MVP

**Goal**: A host produces a copy-paste invite code; a peer joins via that code and connects.

**Independent Test**: Take node A's invite code, POST it to node B's `/peers/join`, confirm A appears in B's `/peers`.

### Tests for User Story 1 (write first, must fail) ⚠️

- [X] T006 [P] [US1] Encode/Decode round-trip + IsPublic tests in internal/invite/invite_test.go (`TestEncodeDecodeRoundTrip`, `TestIsPublic`).
- [X] T007 [P] [US1] Loopback-filter test in internal/invite/invite_test.go (`TestEncodeFiltersLoopback`).
- [X] T008 [P] [US1] Malformed/version rejection tests in internal/invite/invite_test.go (`TestDecodeMalformed`, `TestDecodeUnsupportedVersion`).
- [X] T009 [P] [US1] API tests in internal/api/handlers_test.go: `GET /peers/invite` returns a `music:join:` code; `POST /peers/join` with malformed code → 400; self-invite → 400. Use httptest + a wired invite factory (no network).

### Implementation for User Story 1

- [X] T010 [US1] Implement `Encode`, `Decode`, `IsPublic` and loopback filtering in internal/invite/invite.go per contracts/invite-package.md (make T006–T008 pass).
- [X] T011 [US1] Add request/response models in internal/api/models.go: `JoinRequest{Invite string}`, `InviteResponse{Invite, PeerID string; Reachable bool; Note string}`.
- [X] T012 [US1] Add `InviteFactory func() (string, bool, string)` (code, reachable, note) and `Joiner func(ctx, code string) (peerID string, err error)` fields to api.Config + Server in internal/api/server.go; register routes `GET /peers/invite` and `POST /peers/join`.
- [X] T013 [US1] Implement `handleGetInvite` and `handleJoin` in internal/api/handlers.go with swaggo annotations; validate input, reject self/malformed (400), bound the dial timeout (≤30s), increment `PeersJoined`; make T009 pass.
- [X] T014 [US1] Wire factory + joiner in cmd/node/main.go using internal/invite (Encode from host ID + `host.Addrs()`/observed addrs; Decode + `host.Connect` for join) and print the node's invite code prominently at startup.
- [X] T015 [US1] Add CLI `--join <code>` flag in cmd/node/main.go that decodes and connects on startup with a bounded context.

**Checkpoint**: US1 fully functional — invite shown, fetchable, and join works (LAN + auto-port-mapped internet).

---

## Phase 4: User Story 2 - Zero-config discovery on the same network (Priority: P2)

**Goal**: Two LAN nodes auto-connect with no code exchanged (preserve existing behavior).

**Independent Test**: Start two nodes on one LAN with no invite; confirm `/peers` shows ≥1 peer.

- [X] T016 [US2] Regression check: confirm mDNS auto-connect still works after the host NAT changes (manual two-node LAN run per quickstart Scenario 7); document result.
- [X] T017 [P] [US2] Confirm DHT rendezvous advertise/find still runs (no changes expected) — verify via startup logs `dht-discovery: advertising under namespace`.

**Checkpoint**: Automatic discovery unaffected by invite codes.

---

## Phase 5: User Story 3 - Clear guidance when direct connection impossible (Priority: P3)

**Goal**: Honest reachability messaging and bounded, clear failures.

**Independent Test**: Force a local-only node; confirm `/peers/invite` reports `reachable=false` with a remedy note; confirm a bad/unreachable join fails fast.

- [X] T018 [P] [US3] Test in internal/api/handlers_test.go: when the invite factory reports not-reachable, `GET /peers/invite` returns `reachable=false` and a non-empty `note`.
- [X] T019 [US3] Implement reachability flag + remedy note (use `invite.IsPublic`) in the factory (cmd/node/main.go) and surface `note` in `handleGetInvite`; also print the note at startup when local-only. Set `Reachability` gauge.
- [X] T020 [US3] Ensure `handleJoin` returns a clear bounded-timeout error (not a hang) when no address connects; add/adjust the test asserting a prompt error response.

**Checkpoint**: Users get actionable feedback instead of silent failure.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T021 [P] Regenerate Swagger: `swag init -g cmd/node/main.go -o docs --parseDependency --parseInternal`; verify `/peers/invite` and `/peers/join` appear in docs/swagger.json.
- [X] T022 [P] Update README.md: invite-code connection section (get code, share, join, CLI `--join`) and the NAT/UPnP reachability note + fallback.
- [X] T023 Run `go test ./...` and `go vet ./...` — all green.
- [X] T024 Execute quickstart.md scenarios 1–7 against built binaries; confirm expected outcomes (two-node join, listen, bad-input rejection, LAN zero-config).

---

## Dependencies & Execution Order

- **Setup (Phase 1)**: T001 → T002.
- **Foundational (Phase 2)**: after Setup; T003, T004 [P], then T005. BLOCKS user stories.
- **US1 (Phase 3)**: after Foundational. Tests T006–T009 [P] before impl T010–T015. T010 (invite pkg) before T013/T014. T011/T012 before T013.
- **US2 (Phase 4)**: after Foundational; independent of US1 (verification-only).
- **US3 (Phase 5)**: builds on US1 factory/handlers (T013/T014).
- **Polish (Phase 6)**: after the stories you intend to ship.

## Parallel Opportunities

- T006, T007, T008, T009 (US1 tests) — different concerns, parallelizable.
- T004 parallel with T003.
- T021, T022 (docs) parallel in Polish.

## Implementation Strategy

- **MVP = US1 (Phase 1 + 2 + 3)**: invite shown, fetched, and join works. Ship/demo here.
- **Increment**: add US3 reachability messaging, then confirm US2 regression, then Polish.

## Notes

- [P] = different files, no incomplete-task dependency.
- Verify each test fails before implementing (TDD, Principle VI).
- No new go.mod dependencies (Principle IX); all libp2p NAT features ship with v0.39.1.
- Commit after each task or logical group.
