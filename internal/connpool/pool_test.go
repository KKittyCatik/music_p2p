package connpool_test

import (
	"testing"

	"github.com/KKittyCatik/music_p2p/internal/connpool"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
)

// newPool creates a Pool with a nil host (only ActiveCount / LeastLoadedPeer
// methods are used in these unit tests – no actual stream creation).
func newPool() *connpool.Pool {
	return connpool.New(nil, "/test/1.0.0")
}

func pid(s string) peer.ID { return peer.ID(s) }

func TestActiveCountUnknown(t *testing.T) {
	p := newPool()
	assert.Equal(t, 0, p.ActiveCount(pid("unknown")))
}

func TestLeastLoadedPeerEmpty(t *testing.T) {
	p := newPool()
	result := p.LeastLoadedPeer(nil)
	assert.Equal(t, peer.ID(""), result)
}

func TestLeastLoadedPeer(t *testing.T) {
	p := newPool()
	// Without any active streams, any peer can be returned as least-loaded.
	candidates := []peer.ID{pid("p1"), pid("p2"), pid("p3")}
	result := p.LeastLoadedPeer(candidates)
	assert.Contains(t, candidates, result)
}
