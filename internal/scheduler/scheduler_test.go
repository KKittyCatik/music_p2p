package scheduler_test

import (
	"testing"

	"github.com/KKittyCatik/music_p2p/internal/scheduler"
	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
)

func peerID(s string) peer.ID { return peer.ID(s) }

func makeSched() *scheduler.Scheduler {
	return scheduler.NewScheduler(scoring.NewScorer())
}

func TestNewScheduler(t *testing.T) {
	s := makeSched()
	assert.NotNil(t, s)
	assert.Equal(t, 8, s.MaxInflight())
}

func TestSetCID(t *testing.T) {
	s := makeSched()
	s.SetCID("test-cid")
	// Verify indirectly: NextRequests uses the stored CID.
	cp := map[int][]peer.ID{0: {peerID("p1")}}
	s.SetTotalChunks(1)
	s.SetAvailableChunks(cp)
	reqs := s.NextRequests()
	if assert.Len(t, reqs, 1) {
		assert.Equal(t, "test-cid", reqs[0].CID)
	}
}

func TestSetTotalChunks(t *testing.T) {
	s := makeSched()
	s.SetTotalChunks(100)
	// Verify window is clamped to total.
	cp := make(map[int][]peer.ID)
	for i := 0; i < 100; i++ {
		cp[i] = []peer.ID{peerID("p")}
	}
	s.SetAvailableChunks(cp)
	reqs := s.NextRequests()
	assert.LessOrEqual(t, len(reqs), 100)
}

func TestSetCurrentPosition(t *testing.T) {
	s := makeSched()
	s.SetCurrentPosition(5)
	s.SetTotalChunks(20)
	cp := map[int][]peer.ID{5: {peerID("p")}, 6: {peerID("p")}}
	s.SetAvailableChunks(cp)
	reqs := s.NextRequests()
	for _, r := range reqs {
		assert.GreaterOrEqual(t, r.Index, 5)
	}
}

func TestNextRequestsEmpty(t *testing.T) {
	s := makeSched()
	s.SetTotalChunks(10)
	// No available chunks → empty requests.
	reqs := s.NextRequests()
	assert.Empty(t, reqs)
}

func TestNextRequestsBasic(t *testing.T) {
	s := makeSched()
	s.SetTotalChunks(5)
	cp := map[int][]peer.ID{
		0: {peerID("p1")},
		1: {peerID("p2")},
		2: {peerID("p3")},
	}
	s.SetAvailableChunks(cp)
	reqs := s.NextRequests()
	assert.NotEmpty(t, reqs)
	for _, r := range reqs {
		assert.GreaterOrEqual(t, r.Index, 0)
		assert.Less(t, r.Index, 5)
	}
}

func TestNextRequestsPriorityZones(t *testing.T) {
	s := makeSched()
	s.SetTotalChunks(30)

	// Critical zone: 0-4, high: 5-19, prefetch: 20+
	cp := make(map[int][]peer.ID)
	for i := 0; i < 25; i++ {
		cp[i] = []peer.ID{peerID("p")}
	}
	s.SetAvailableChunks(cp)
	reqs := s.NextRequests()

	// First requests should be from the critical zone.
	assert.NotEmpty(t, reqs)
	assert.Equal(t, 0, reqs[0].Priority) // priorityCritical
}

func TestMarkCompleted(t *testing.T) {
	s := makeSched()
	s.SetTotalChunks(5)
	cp := map[int][]peer.ID{0: {peerID("p")}}
	s.SetAvailableChunks(cp)

	reqs := s.NextRequests()
	assert.Len(t, reqs, 1)

	s.MarkCompleted(reqs[0].Index)

	// Should not be in next requests anymore.
	reqs2 := s.NextRequests()
	for _, r := range reqs2 {
		assert.NotEqual(t, 0, r.Index)
	}
}

func TestMarkFailed(t *testing.T) {
	s := makeSched()
	s.SetTotalChunks(5)
	p1 := peerID("p1")
	p2 := peerID("p2")
	cp := map[int][]peer.ID{0: {p1, p2}}
	s.SetAvailableChunks(cp)

	reqs := s.NextRequests()
	assert.Len(t, reqs, 1)
	usedPeer := reqs[0].Peer

	// Mark failed for the peer that was chosen.
	s.MarkFailed(0, usedPeer)

	// Next request for chunk 0 should use the other peer.
	reqs2 := s.NextRequests()
	for _, r := range reqs2 {
		if r.Index == 0 {
			assert.NotEqual(t, usedPeer, r.Peer)
		}
	}
}

func TestCongestionControl(t *testing.T) {
	s := makeSched()
	initial := s.MaxInflight()

	// Trigger 3 consecutive failures to halve maxInflight.
	for i := 0; i < 3; i++ {
		s.MarkFailed(i, peerID("p"))
	}
	assert.Less(t, s.MaxInflight(), initial)
}

func TestResetTo(t *testing.T) {
	s := makeSched()
	s.SetTotalChunks(20)
	cp := map[int][]peer.ID{0: {peerID("p")}, 1: {peerID("p")}}
	s.SetAvailableChunks(cp)

	s.NextRequests() // puts chunks into inflight
	s.ResetTo(5)
	s.SetCurrentPosition(5)

	// After reset, next requests should start from new position.
	cp2 := map[int][]peer.ID{5: {peerID("p")}, 6: {peerID("p")}}
	s.SetAvailableChunks(cp2)
	reqs := s.NextRequests()
	for _, r := range reqs {
		assert.GreaterOrEqual(t, r.Index, 5)
	}
}

func TestMaxInflight(t *testing.T) {
	s := makeSched()
	s.SetTotalChunks(100)

	cp := make(map[int][]peer.ID)
	for i := 0; i < 100; i++ {
		cp[i] = []peer.ID{peerID("p")}
	}
	s.SetAvailableChunks(cp)

	// Non-critical chunks should not exceed MaxInflight.
	reqs := s.NextRequests()
	nonCritical := 0
	for _, r := range reqs {
		if r.Priority > 0 {
			nonCritical++
		}
	}
	assert.LessOrEqual(t, nonCritical, s.MaxInflight())
}
