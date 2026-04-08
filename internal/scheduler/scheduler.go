package scheduler

import (
	"sort"
	"sync"

	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// initialMaxInflight is the starting concurrency limit.
	initialMaxInflight = 8
	// minMaxInflight is the floor for dynamic adjustment.
	minMaxInflight = 2
	// maxMaxInflight is the ceiling for dynamic adjustment.
	maxMaxInflight = 32
	// window is the total lookahead of chunks considered for scheduling.
	window = 50

	// Priority zone boundaries (distance from current position).
	zoneCriticalEnd  = 5
	zoneHighEnd      = 20
	// Prefetch zone starts at zoneHighEnd and goes to window.

	// congestionWindow is the number of recent outcomes tracked.
	congestionWindow = 20
)

// priority levels for sorting.
const (
	priorityCritical = 0
	priorityHigh     = 1
	priorityPrefetch = 2
)

// ChunkRequest describes a single chunk fetch to dispatch.
type ChunkRequest struct {
	CID      string
	Index    int
	Peer     peer.ID
	Priority int // priorityCritical < priorityHigh < priorityPrefetch
}

// Scheduler decides which chunks to fetch from which peers.
type Scheduler struct {
	mu          sync.Mutex
	scorer      *scoring.Scorer
	cid         string
	totalChunks int
	currentPos  int
	// chunkPeers maps chunk index → list of peers that have it
	chunkPeers map[int][]peer.ID
	inflight   map[int]struct{}
	completed  map[int]struct{}
	// failed tracks per-chunk per-peer failures
	failed map[int]map[peer.ID]struct{}

	// Congestion-control state.
	maxInflight    int
	recentOutcomes []bool // true=success, false=failure (sliding window)
	consecutiveFail int
}

// NewScheduler creates a Scheduler backed by the given Scorer.
func NewScheduler(scorer *scoring.Scorer) *Scheduler {
	return &Scheduler{
		scorer:      scorer,
		chunkPeers:  make(map[int][]peer.ID),
		inflight:    make(map[int]struct{}),
		completed:   make(map[int]struct{}),
		failed:      make(map[int]map[peer.ID]struct{}),
		maxInflight: initialMaxInflight,
	}
}

// SetCID sets the content identifier being streamed.
func (s *Scheduler) SetCID(cid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cid = cid
}

// SetTotalChunks records the total number of chunks in the track.
func (s *Scheduler) SetTotalChunks(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalChunks = n
}

// SetCurrentPosition updates the current playback position (chunk index).
func (s *Scheduler) SetCurrentPosition(pos int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentPos = pos
}

// SetAvailableChunks updates the map of which peers hold which chunks.
func (s *Scheduler) SetAvailableChunks(chunkPeers map[int][]peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunkPeers = chunkPeers
}

// recordOutcome appends to the sliding congestion window and adjusts maxInflight.
// Must be called with s.mu held.
func (s *Scheduler) recordOutcome(success bool) {
	s.recentOutcomes = append(s.recentOutcomes, success)
	if len(s.recentOutcomes) > congestionWindow {
		s.recentOutcomes = s.recentOutcomes[1:]
	}

	if !success {
		s.consecutiveFail++
		// On consecutive failures, halve maxInflight (floor at minMaxInflight).
		if s.consecutiveFail >= 3 {
			s.maxInflight /= 2
			if s.maxInflight < minMaxInflight {
				s.maxInflight = minMaxInflight
			}
			s.consecutiveFail = 0
		}
		return
	}
	s.consecutiveFail = 0

	// On sustained success in the window, ramp up slowly.
	if len(s.recentOutcomes) >= congestionWindow {
		successCount := 0
		for _, ok := range s.recentOutcomes {
			if ok {
				successCount++
			}
		}
		if float64(successCount)/float64(congestionWindow) >= 0.9 {
			if s.maxInflight < maxMaxInflight {
				s.maxInflight++
			}
		}
	}
}

// chunkPriority returns the priority zone for a chunk at distance d from currentPos.
func chunkPriority(d int) int {
	switch {
	case d < zoneCriticalEnd:
		return priorityCritical
	case d < zoneHighEnd:
		return priorityHigh
	default:
		return priorityPrefetch
	}
}

// NextRequests returns new ChunkRequests to dispatch, honouring the dynamic
// maxInflight limit.  Critical-zone chunks may exceed the normal cap slightly
// to ensure they are fetched ASAP.
func (s *Scheduler) NextRequests() []ChunkRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	available := s.maxInflight - len(s.inflight)
	if available <= 0 {
		// Allow critical chunks to go through even when at capacity.
		available = 0
	}

	type candidate struct {
		index    int
		peers    []peer.ID
		distance int
		priority int
	}

	var candidates []candidate
	end := s.currentPos + window
	if s.totalChunks > 0 && end > s.totalChunks {
		end = s.totalChunks
	}

	for idx := s.currentPos; idx < end; idx++ {
		if _, done := s.completed[idx]; done {
			continue
		}
		if _, flying := s.inflight[idx]; flying {
			continue
		}
		peers := s.chunkPeers[idx]
		if len(peers) == 0 {
			continue
		}
		// Filter out peers that have already failed this chunk.
		var viable []peer.ID
		for _, p := range peers {
			if _, bad := s.failed[idx][p]; !bad {
				viable = append(viable, p)
			}
		}
		if len(viable) == 0 {
			continue
		}
		dist := idx - s.currentPos
		candidates = append(candidates, candidate{
			index:    idx,
			peers:    viable,
			distance: dist,
			priority: chunkPriority(dist),
		})
	}

	// Sort: priority first (lower = more urgent), then distance, then rarity.
	sort.Slice(candidates, func(i, j int) bool {
		ci, cj := candidates[i], candidates[j]
		if ci.priority != cj.priority {
			return ci.priority < cj.priority
		}
		if ci.distance != cj.distance {
			return ci.distance < cj.distance
		}
		return len(ci.peers) < len(cj.peers)
	})

	var requests []ChunkRequest
	for _, c := range candidates {
		// Critical chunks bypass the normal inflight cap to avoid stalls.
		if c.priority != priorityCritical && len(requests) >= available {
			break
		}
		best := s.scorer.BestPeers(1, c.peers)
		if len(best) == 0 {
			continue
		}
		requests = append(requests, ChunkRequest{
			CID:      s.cid,
			Index:    c.index,
			Peer:     best[0],
			Priority: c.priority,
		})
		s.inflight[c.index] = struct{}{}
	}
	return requests
}

// MarkCompleted removes a chunk from inflight, records it as done, and
// updates the congestion window.
func (s *Scheduler) MarkCompleted(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, index)
	s.completed[index] = struct{}{}
	s.recordOutcome(true)
}

// MarkFailed removes a chunk from inflight, records the peer failure, and
// updates the congestion window.
func (s *Scheduler) MarkFailed(index int, p peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, index)
	if s.failed[index] == nil {
		s.failed[index] = make(map[peer.ID]struct{})
	}
	s.failed[index][p] = struct{}{}
	s.recordOutcome(false)
}

// IsCompleted returns true if the chunk has been successfully downloaded.
func (s *Scheduler) IsCompleted(index int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.completed[index]
	return ok
}

// ResetTo resets the scheduler window and inflight state to start from pos.
// Completed chunks are preserved.
func (s *Scheduler) ResetTo(pos int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentPos = pos
	s.inflight = make(map[int]struct{})
}

// MaxInflight returns the current dynamic concurrency limit.
func (s *Scheduler) MaxInflight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxInflight
}
