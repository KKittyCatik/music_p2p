package invite_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KKittyCatik/music_p2p/internal/invite"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// a known-valid peer ID for tests (no host required).
const testPeerID = "12D3KooWGEcvsAdKjVqiFKx2kEoLicxuk4VGdHcAqwzjYvZ6sFGx"

func mustAddr(t *testing.T, s string) ma.Multiaddr {
	t.Helper()
	a, err := ma.NewMultiaddr(s)
	require.NoError(t, err)
	return a
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	id, err := peer.Decode(testPeerID)
	require.NoError(t, err)
	lan := mustAddr(t, "/ip4/192.168.1.10/tcp/4001")
	pub := mustAddr(t, "/ip4/1.2.3.4/tcp/4001")

	code := invite.Encode(id, []ma.Multiaddr{lan, pub})
	assert.True(t, strings.HasPrefix(code, invite.Prefix), "code must carry prefix")

	info, err := invite.Decode(code)
	require.NoError(t, err)
	assert.Equal(t, id, info.ID)
	assert.ElementsMatch(t, []string{lan.String(), pub.String()}, addrStrings(info.Addrs))
}

func TestEncodeFiltersLoopback(t *testing.T) {
	id, err := peer.Decode(testPeerID)
	require.NoError(t, err)
	loop := mustAddr(t, "/ip4/127.0.0.1/tcp/4001")
	lan := mustAddr(t, "/ip4/192.168.1.10/tcp/4001")

	info, err := invite.Decode(invite.Encode(id, []ma.Multiaddr{loop, lan}))
	require.NoError(t, err)
	assert.Equal(t, []string{lan.String()}, addrStrings(info.Addrs), "loopback must be filtered")
}

func TestIsPublic(t *testing.T) {
	pub := mustAddr(t, "/ip4/1.2.3.4/tcp/4001")
	lan := mustAddr(t, "/ip4/192.168.1.10/tcp/4001")
	loop := mustAddr(t, "/ip4/127.0.0.1/tcp/4001")

	assert.True(t, invite.IsPublic([]ma.Multiaddr{lan, pub}))
	assert.False(t, invite.IsPublic([]ma.Multiaddr{lan}))
	assert.False(t, invite.IsPublic([]ma.Multiaddr{loop}))
	assert.False(t, invite.IsPublic(nil))
}

func TestDecodeMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"no prefix":     "just-some-text",
		"bad base64":    invite.Prefix + "!!!not-base64!!!",
		"not json":      invite.Prefix + base64.RawURLEncoding.EncodeToString([]byte("not json")),
	}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := invite.Decode(code)
			assert.Error(t, err)
		})
	}
}

func TestDecodeUnsupportedVersion(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"v": 999, "id": testPeerID, "addrs": []string{}})
	code := invite.Prefix + base64.RawURLEncoding.EncodeToString(payload)
	_, err := invite.Decode(code)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "version")
}

func addrStrings(addrs []ma.Multiaddr) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}
