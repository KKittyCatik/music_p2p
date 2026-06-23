# Feature Specification: Invite-Code Node Connection

**Feature Branch**: `001-invite-connect`

**Created**: 2026-06-23

**Status**: Draft

**Input**: User description: "Connect several nodes conveniently and understandably for the user, via invite codes, no servers, working both on LAN and across the internet."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Share an invite and connect a friend (Priority: P1)

A user running a node wants a friend on another machine to join them. The host
copies a single short invite code shown by their node and sends it to the friend
over any channel (chat, email). The friend pastes the code into their node, and
the two nodes connect — after which the friend can already browse and listen to
the host's shared tracks.

**Why this priority**: This is the core of the feature. Without a simple,
copy-paste connection that works across machines, "test on humans" is impossible.
Everything else is a refinement of this flow.

**Independent Test**: Start two nodes on different machines (or two networks),
take the invite code from node A, submit it to node B, and confirm node B reports
node A as a connected peer and can list node A's metadata.

**Acceptance Scenarios**:

1. **Given** node A is running, **When** the user requests A's invite code,
   **Then** A returns one self-contained code string that encodes everything node
   B needs to reach A.
2. **Given** the user has A's invite code, **When** they submit it to node B,
   **Then** B connects to A and A appears in B's connected-peer list.
3. **Given** A and B are on the same local network, **When** B joins via A's
   invite code, **Then** the connection succeeds without any router configuration.
4. **Given** A is behind a home router that permits automatic port mapping,
   **When** B on a different network joins via A's invite code, **Then** the
   connection succeeds without the user manually forwarding ports.

---

### User Story 2 - Zero-config discovery on the same network (Priority: P2)

Two users on the same Wi-Fi want their nodes to find each other without exchanging
anything at all.

**Why this priority**: Removes all friction for the most common demo setup (same
room, same network). It already partially exists and should keep working.

**Independent Test**: Start two nodes on the same LAN with no invite exchange and
confirm they discover and connect to each other automatically.

**Acceptance Scenarios**:

1. **Given** two nodes on the same local network, **When** both are started,
   **Then** they discover and connect to each other automatically within a short
   time, with no codes exchanged.

---

### User Story 3 - Clear guidance when a direct connection is impossible (Priority: P3)

When a host's network cannot be reached automatically (e.g. automatic port
mapping is unavailable and the network type blocks direct connection), the user
should understand why and what to do, rather than facing a silent failure.

**Why this priority**: Honest, actionable feedback preserves trust during human
testing and prevents confused bug reports. It is a refinement, not a blocker.

**Independent Test**: Start a node in an environment where it cannot become
reachable and confirm it clearly reports its reachability state and a suggested
remedy instead of silently presenting an unusable invite.

**Acceptance Scenarios**:

1. **Given** a node cannot determine any externally reachable address, **When**
   the user requests its invite code, **Then** the node still returns a code usable
   on the local network and clearly indicates that remote peers may not be able to
   connect plus what the user can do about it.
2. **Given** a join attempt cannot reach any address in an invite code, **When**
   the attempt times out, **Then** the joining user receives a clear failure
   message rather than an indefinite hang.

---

### Edge Cases

- What happens when an invite code is malformed, truncated, or from an
  incompatible version? → The join is rejected with a clear validation error.
- What happens when a user submits their own node's invite code? → It is rejected
  (cannot connect to self) without entering an error state.
- What happens when a node has multiple addresses (LAN + public)? → The invite
  carries all reachable addresses and the joiner tries them until one connects.
- What happens when the host's address changes after an invite was shared (e.g.
  new IP)? → The stale invite may fail; the host can generate a fresh code.
- What happens when a peer is already connected and the same invite is submitted
  again? → The operation is idempotent and reports success.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A node MUST be able to produce a single self-contained invite code
  that encodes its identity and all of its currently reachable addresses.
- **FR-002**: The invite code MUST be a compact, copy-paste-friendly single string
  that survives transport through chat and email without manual editing.
- **FR-003**: A node MUST be able to accept an invite code and establish a
  connection to the node it identifies.
- **FR-004**: Joining via an invite code MUST work when both nodes are on the same
  local network with no router configuration.
- **FR-005**: Joining via an invite code MUST work across different networks when
  the host's router permits automatic port mapping, without the user manually
  forwarding ports.
- **FR-006**: The node MUST attempt automatic external reachability (automatic
  port mapping and direct NAT traversal) without requiring any always-on
  server operated by the project.
- **FR-007**: A node MUST display its own invite code prominently at startup so the
  user can copy it without inspecting logs or computing addresses by hand.
- **FR-008**: Invalid, truncated, incompatible, or self-referential invite codes
  MUST be rejected with a clear, actionable message and MUST NOT crash the node.
- **FR-009**: A join attempt that cannot reach any address MUST fail within a
  bounded time with a clear message, never hang indefinitely.
- **FR-010**: When a node cannot determine any externally reachable address, it
  MUST still produce a locally usable invite code and clearly communicate the
  limited reachability and a suggested remedy.
- **FR-011**: Existing automatic same-network discovery MUST continue to work
  unchanged alongside invite codes.
- **FR-012**: The invite code MUST NOT require the user to know or type IP
  addresses, ports, or node identifiers manually.

### Key Entities *(include if feature involves data)*

- **Invite Code**: A shareable token representing how to reach one node. Contains
  the node's stable identity and its list of reachable network addresses. Self-
  contained (no external lookup needed) and version-tagged so incompatible codes
  can be rejected cleanly.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A new user can connect their node to a friend's node using only a
  copied invite code, in under 1 minute, without reading documentation.
- **SC-002**: On the same local network, two nodes connect with zero exchanged
  information.
- **SC-003**: Across two different home networks where automatic port mapping is
  available, an invite-code connection succeeds without any manual router setup.
- **SC-004**: A malformed or unreachable invite never causes a hang; the user gets
  a clear result (success or actionable failure) within 30 seconds.
- **SC-005**: The user obtains their shareable invite code from a single, obvious
  place (startup output or one API call) without manual address assembly.

## Assumptions

- No always-on infrastructure (bootstrap/relay servers) will be operated by the
  project; connectivity relies on local discovery, automatic router port mapping,
  and direct NAT traversal only. As a consequence, some restrictive network types
  (e.g. symmetric NAT with port mapping disabled) may not be reachable without the
  user manually forwarding a port — this is an accepted limitation.
- Users exchange the invite code through an out-of-band channel they already trust
  (chat, email); securing that channel is out of scope.
- A node's reachable addresses are reasonably stable for the duration of a sharing
  session; if they change, the host re-issues a code.
- Invite codes are single-use convenience artifacts, not long-lived credentials;
  no revocation mechanism is required for this iteration.
