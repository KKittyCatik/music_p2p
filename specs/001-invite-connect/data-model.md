# Data Model: Invite-Code Node Connection

## Entity: Invite

A self-contained, shareable token describing how to reach one node. Computed from
live host state at request time; never persisted.

### Wire form (inside the code)

| Field   | Type       | Required | Description |
|---------|------------|----------|-------------|
| `v`     | int        | yes      | Format version. Current = `1`. Used to reject incompatible codes. |
| `id`    | string     | yes      | Node's libp2p peer ID (base58). |
| `addrs` | string[]   | yes      | Reachable multiaddrs (no `/p2p/...` suffix needed; loopback excluded). May be empty. |

### Encoded form

```
music:join:<base64url( JSON({v,id,addrs}) )>
```

- Prefix `music:join:` makes the token self-identifying.
- `base64url` (no padding) is copy-paste safe across chat/email.

### Validation rules (decode side)

1. String MUST start with the `music:join:` prefix → else reject ("not an invite code").
2. The remainder MUST be valid base64url decoding to valid JSON → else reject ("malformed invite").
3. `v` MUST equal the supported version → else reject ("unsupported invite version N").
4. `id` MUST decode as a valid peer ID → else reject ("invalid peer id").
5. Each entry in `addrs` MUST parse via the multiaddr parser; unparseable entries
   are dropped. If `id` is valid the invite is still usable even with zero addrs
   (caller may already know addresses via discovery), but join will fail fast if no
   address is reachable.
6. The decoded peer ID MUST NOT equal the local node's ID → caller rejects self-join.

### Derivation rules (encode side)

- `id` = host peer ID.
- `addrs` = host listen + observed addresses, with loopback (`127.0.0.0/8`, `::1`)
  removed. Public/observed addresses (from AutoNAT/identify) included when known.
- Reachability flag (for FR-010 messaging) = public iff at least one non-private
  address is present; otherwise local-only.

## Relationships

- An **Invite** references exactly one node (by peer ID).
- Decoding an Invite yields a connection target equivalent to a libp2p
  `peer.AddrInfo` (peer ID + addresses).

## State / lifecycle

- Stateless and ephemeral: generated on demand, valid only while the node's
  addresses remain current. No revocation, no storage, no expiry field in v1.
