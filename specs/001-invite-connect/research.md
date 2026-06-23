# Research: Invite-Code Node Connection

No open `NEEDS CLARIFICATION` items — the user fixed the key constraints (no
servers, invite codes, LAN + internet). This records the design decisions and the
alternatives weighed.

## Decision 1 — NAT traversal without project-operated servers

**Decision**: On the libp2p host enable, in order of importance:
1. `libp2p.NATPortMap()` — automatic UPnP / NAT-PMP port mapping so the node opens
   its listen port on a cooperating home router and becomes directly dialable.
2. AutoNAT (`libp2p.EnableNATService()`) + identify — so the node learns its
   observed external address and can advertise it in the invite.
3. `libp2p.EnableHolePunching()` (DCUtR) — best-effort direct hole punching.

**Rationale**: UPnP/NAT-PMP is the single highest-leverage, zero-infrastructure
mechanism: most consumer routers support it and it makes the node reachable with no
user action. AutoNAT supplies the real external multiaddr to embed in the invite.

**Alternatives considered**:
- *Public bootstrap + relay node*: most convenient ("just works") but requires an
  always-on server — explicitly rejected by the user and in tension with Principle I.
- *Static public relays (third-party)*: no infra of our own but fragile and depends
  on others' uptime; DCUtR also needs a relay to coordinate, so hole punching is
  best-effort only. Documented as an accepted limitation, not a dependency.
- *Manual port forwarding only*: status quo; too unfriendly for "test on humans".

**Accepted limitation**: symmetric NAT or routers with UPnP disabled may remain
unreachable without manual port forwarding (FR-010 surfaces this to the user).

## Decision 2 — Invite code format

**Decision**: `music:join:<base64url(JSON)>` where the JSON is
`{ "v": 1, "id": "<peerID>", "addrs": ["<multiaddr>", ...] }`. A small version
tag enables clean rejection of incompatible codes.

**Rationale**: A single prefixed token is unambiguous to detect and copy-paste safe
(base64url has no characters mangled by chat/email). JSON keeps it debuggable and
trivially extensible. Carrying *all* reachable addresses lets the joiner try LAN
and public addresses and connect on whichever works (covers FR-004/005/012).

**Alternatives considered**:
- *Raw multiaddr string* (`/ip4/.../p2p/<id>`): works for one address but awkward
  with multiple addresses and exposes raw internals to users.
- *peer.AddrInfo p2p-encoded form*: compact but single-address-oriented and less
  obvious as a "code"; harder to version.

## Decision 3 — Address filtering for the invite

**Decision**: Exclude loopback (`127.0.0.0/8`, `::1`) from the invite. Include LAN
private and public/observed addresses. If the resulting set is empty, still emit a
code (loopback-only / local) and flag limited reachability (FR-010).

**Rationale**: Loopback is never useful to a remote or LAN peer. Keeping LAN +
public covers both same-network and cross-internet joins from one code.

## Decision 4 — Join semantics & timeout

**Decision**: `POST /peers/join` decodes the invite, then dials all addresses via a
context with a bounded timeout (default 30 s, aligning with SC-004). Success if any
address connects. Reject self-invites and malformed codes with 400; report
unreachable with a clear error. Idempotent when already connected.

**Rationale**: Bounded dial prevents indefinite hangs (FR-009); trying all addresses
maximizes success across mixed network conditions.

## Decision 5 — Observability

**Decision**: Add `music_p2p_peers_joined_total` (counter, labeled by result
success/failure) and `music_p2p_reachability` (gauge: 0 unknown/private, 1 public)
to internal/metrics; log join attempts/results via zap (Principle V).

## Decision 6 — Keep existing discovery

**Decision**: No changes to mDNS or DHT rendezvous. NAT options are additive on the
host. Invite codes are an additional, explicit path that coexists with automatic
discovery (FR-011).
