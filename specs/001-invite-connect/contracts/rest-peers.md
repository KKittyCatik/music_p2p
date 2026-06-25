# Contract: REST peer-connection endpoints

All under `/api/v1`. Standard envelope: `{"success": bool, "data": ..., "error": ""}`.

## GET /peers/invite

Returns this node's shareable invite code.

**Request**: none.

**Response 200**:
```json
{
  "success": true,
  "data": {
    "invite": "music:join:eyJ2IjoxLCJpZCI6IjEyRDNL...",
    "peer_id": "12D3KooW...",
    "reachable": true,
    "note": ""
  },
  "error": ""
}
```

- `invite`: the copy-paste code (FR-001, FR-002).
- `reachable`: `false` when only private/loopback addresses are known; in that case
  `note` carries the limited-reachability guidance (FR-010), e.g. "Only local
  addresses known — remote peers may not connect. Enable UPnP or forward port 4001."

**Response 503**: host not available → `{"success":false,...,"error":"host not available"}`.

## POST /peers/join

Connects this node to the peer described by an invite code.

**Request**:
```json
{ "invite": "music:join:eyJ2IjoxLCJpZCI6..." }
```

**Response 200** (connected, or already connected — idempotent):
```json
{ "success": true, "data": { "peer_id": "12D3KooW...", "connected": true }, "error": "" }
```

**Response 400** — invalid input (FR-008):
- malformed / wrong prefix / bad base64 / bad JSON → `"malformed invite"`
- unsupported version → `"unsupported invite version N"`
- invalid peer id → `"invalid peer id"`
- self-invite → `"cannot join your own node"`

**Response 504/500** — could not reach any address within the bounded timeout
(FR-009): `{"success":false,...,"error":"could not connect to peer: <reason>"}`.

## Behavioural notes

- Join dials all addresses from the invite under a bounded context (≤ 30 s, SC-004).
- Success if any address connects.
- These endpoints coexist with the existing `POST /peers/connect` (raw multiaddr),
  which remains for advanced use.
