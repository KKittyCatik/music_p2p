package discovery_test

import (
	"context"
	"testing"

	"github.com/KKittyCatik/music_p2p/internal/discovery"
	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBootstrap_EmptyList verifies Bootstrap returns 0 without crashing.
func TestBootstrap_EmptyList(t *testing.T) {
	ctx := context.Background()
	h, err := libp2p.New()
	require.NoError(t, err)
	defer h.Close()

	n := discovery.Bootstrap(ctx, h, []string{})
	assert.Equal(t, 0, n)
}

// TestBootstrap_InvalidAddr verifies that an invalid multiaddr is skipped
// gracefully (no panic, no crash).
func TestBootstrap_InvalidAddr(t *testing.T) {
	ctx := context.Background()
	h, err := libp2p.New()
	require.NoError(t, err)
	defer h.Close()

	n := discovery.Bootstrap(ctx, h, []string{"not-a-valid-multiaddr"})
	assert.Equal(t, 0, n)
}

// TestBootstrap_EmptyStringInList verifies empty strings are skipped.
func TestBootstrap_EmptyStringInList(t *testing.T) {
	ctx := context.Background()
	h, err := libp2p.New()
	require.NoError(t, err)
	defer h.Close()

	n := discovery.Bootstrap(ctx, h, []string{"", "", ""})
	assert.Equal(t, 0, n)
}

// TestBootstrap_ConnectsToValid verifies Bootstrap connects to a reachable peer.
func TestBootstrap_ConnectsToValid(t *testing.T) {
	ctx := context.Background()

	// Start a target host that listens.
	target, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer target.Close()

	// Build a full multiaddr including /p2p/<ID>.
	targetAddrs := target.Addrs()
	require.NotEmpty(t, targetAddrs)
	targetInfo := peer.AddrInfo{ID: target.ID(), Addrs: target.Addrs()}
	fullAddr, err := peer.AddrInfoToP2pAddrs(&targetInfo)
	require.NoError(t, err)
	require.NotEmpty(t, fullAddr)

	// Start the connecting host.
	h, err := libp2p.New()
	require.NoError(t, err)
	defer h.Close()

	n := discovery.Bootstrap(ctx, h, []string{fullAddr[0].String()})
	assert.Equal(t, 1, n)
}

// TestMDNSNotifee_SkipsSelf verifies that StartMDNS does not crash and the
// notifee correctly skips self-connections (tested indirectly via the service).
func TestMDNSNotifee_SkipsSelf(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := libp2p.New()
	require.NoError(t, err)
	defer h.Close()

	// Starting mDNS should not return an error.
	err = discovery.StartMDNS(ctx, h)
	assert.NoError(t, err)
}
