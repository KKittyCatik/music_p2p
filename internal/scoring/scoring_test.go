package scoring_test

import (
	"sync"
	"testing"
	"time"

	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
)

// peerID creates a simple peer.ID for testing purposes.
func peerID(s string) peer.ID {
	return peer.ID(s)
}

func TestNewScorer(t *testing.T) {
	sc := scoring.NewScorer()
	assert.NotNil(t, sc)
}

func TestRecordSuccess(t *testing.T) {
	sc := scoring.NewScorer()
	id := peerID("peer-1")

	sc.RecordSuccess(id, 50*time.Millisecond, 1024)
	sc.RecordSuccess(id, 100*time.Millisecond, 2048)

	score := sc.Score(id)
	assert.Greater(t, score, 0.0)
	assert.LessOrEqual(t, score, 1.0)
}

func TestRecordFailure(t *testing.T) {
	sc := scoring.NewScorer()
	id := peerID("peer-2")

	sc.RecordFailure(id)
	sc.RecordFailure(id)

	score := sc.Score(id)
	// All failures → success rate = 0
	assert.Equal(t, 0.0, score)
}

func TestScoreUnknownPeer(t *testing.T) {
	sc := scoring.NewScorer()
	id := peerID("unknown-peer")
	assert.Equal(t, 0.5, sc.Score(id))
}

func TestScoreCalculation(t *testing.T) {
	sc := scoring.NewScorer()
	id := peerID("peer-calc")

	// 1 success, 0 failures → success rate = 1.0
	// latency = 50ms → latencyScore = 1/(1+50/100) = 0.667
	// bytes=1_000_000, time=50ms → bps=20_000_000 → throughputScore=1.0
	// total = 1.0*0.4 + 1.0*0.3 + 0.667*0.3 = 0.4 + 0.3 + 0.2 = 0.9
	sc.RecordSuccess(id, 50*time.Millisecond, 1_000_000)

	score := sc.Score(id)
	assert.Greater(t, score, 0.85)
}

func TestBestPeers(t *testing.T) {
	sc := scoring.NewScorer()
	good := peerID("good")
	bad := peerID("bad")

	sc.RecordSuccess(good, 10*time.Millisecond, 1_000_000)
	sc.RecordFailure(bad)
	sc.RecordFailure(bad)

	best := sc.BestPeers(2, []peer.ID{bad, good})
	assert.Equal(t, good, best[0])
}

func TestBestPeersMoreThanAvailable(t *testing.T) {
	sc := scoring.NewScorer()
	id := peerID("only-one")
	candidates := []peer.ID{id}

	best := sc.BestPeers(10, candidates)
	assert.Equal(t, 1, len(best))
	assert.Equal(t, id, best[0])
}

func TestConcurrentRecording(t *testing.T) {
	sc := scoring.NewScorer()
	const workers = 20
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			sc.RecordSuccess(peerID("p"), time.Duration(n)*time.Millisecond, n*100)
		}(i)
		go func() {
			defer wg.Done()
			sc.RecordFailure(peerID("p"))
		}()
	}
	wg.Wait()
	// Just ensure no race / panic.
	_ = sc.Score(peerID("p"))
}
