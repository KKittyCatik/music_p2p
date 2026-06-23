// Package invite encodes and decodes copy-paste-friendly node connection codes.
//
// An invite code is a single self-contained string of the form
//
//	music:join:<base64url(JSON)>
//
// where the JSON payload is {"v":1,"id":"<peerID>","addrs":["<multiaddr>",...]}.
// It is pure and network-free so it can be unit-tested without a libp2p host.
package invite

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// Version is the current invite format version.
const Version = 1

// Prefix marks an invite code string.
const Prefix = "music:join:"

// Info is the decoded target of an invite.
type Info struct {
	ID    peer.ID
	Addrs []ma.Multiaddr
}

// payload is the JSON wire form carried inside the encoded code.
type payload struct {
	V     int      `json:"v"`
	ID    string   `json:"id"`
	Addrs []string `json:"addrs"`
}

// Encode builds an invite code from a peer ID and its addresses. Loopback
// addresses are filtered out so the code only carries addresses a remote or LAN
// peer could actually dial.
func Encode(id peer.ID, addrs []ma.Multiaddr) string {
	p := payload{V: Version, ID: id.String()}
	for _, a := range addrs {
		if a == nil || manet.IsIPLoopback(a) {
			continue
		}
		p.Addrs = append(p.Addrs, a.String())
	}
	raw, _ := json.Marshal(p) // payload has only string/int fields; never errors
	return Prefix + base64.RawURLEncoding.EncodeToString(raw)
}

// Decode parses an invite code into Info, applying all validation rules. It
// returns a descriptive error for malformed, unsupported, or invalid codes and
// never panics. Unparseable individual addresses are dropped; a valid peer ID is
// required.
func Decode(code string) (Info, error) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, Prefix) {
		return Info{}, fmt.Errorf("not an invite code (missing %q prefix)", Prefix)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, Prefix))
	if err != nil {
		return Info{}, fmt.Errorf("malformed invite: %w", err)
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Info{}, fmt.Errorf("malformed invite: %w", err)
	}
	if p.V != Version {
		return Info{}, fmt.Errorf("unsupported invite version %d (this node speaks %d)", p.V, Version)
	}
	id, err := peer.Decode(p.ID)
	if err != nil {
		return Info{}, fmt.Errorf("invalid peer id: %w", err)
	}
	info := Info{ID: id}
	for _, s := range p.Addrs {
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			continue // drop unparseable entries rather than failing the whole code
		}
		info.Addrs = append(info.Addrs, a)
	}
	return info, nil
}

// IsPublic reports whether at least one address is a non-private, non-loopback
// (publicly routable) address. Used to set the node's reachability flag.
func IsPublic(addrs []ma.Multiaddr) bool {
	for _, a := range addrs {
		if a != nil && manet.IsPublicAddr(a) {
			return true
		}
	}
	return false
}
