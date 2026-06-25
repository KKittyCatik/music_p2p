# Contract: internal/invite package

Pure, network-free encode/decode of invite codes. Unit-testable without a host
(Principle IV).

## Public API

```go
package invite

// Version is the current invite format version.
const Version = 1

// Prefix marks an invite code string.
const Prefix = "music:join:"

// Info is the decoded target of an invite.
type Info struct {
    ID    peer.ID
    Addrs []ma.Multiaddr
}

// Encode builds an invite code from a peer ID and its addresses.
// Loopback addresses are filtered out. Returns the "music:join:<base64url>" string.
func Encode(id peer.ID, addrs []ma.Multiaddr) string

// Decode parses an invite code into Info, applying all validation rules.
// Returns a descriptive error for malformed/unsupported/invalid codes.
func Decode(code string) (Info, error)

// IsPublic reports whether at least one address is a non-private, non-loopback
// address (used to set the reachability flag/messaging).
func IsPublic(addrs []ma.Multiaddr) bool
```

## Guarantees

- **Round-trip**: `Decode(Encode(id, addrs))` yields the same peer ID and the same
  address set minus loopback. (test: `TestEncodeDecodeRoundTrip`)
- **Loopback filtered**: addresses on `127.0.0.0/8` / `::1` never appear in output.
  (test: `TestEncodeFiltersLoopback`)
- **Malformed rejected**: empty string, wrong prefix, bad base64, bad JSON each
  return an error, never panic. (test: `TestDecodeMalformed`)
- **Version enforced**: a code with `v != Version` is rejected with a clear message.
  (test: `TestDecodeUnsupportedVersion`)
- **Deterministic & pure**: no I/O, no globals, safe for concurrent use.

## Non-goals

- No host construction, no dialing (that lives in the API/CLI layer).
- No persistence, no expiry, no signing in v1.
