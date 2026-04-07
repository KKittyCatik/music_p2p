package scheduler

import (
	"sort"
	"sync"

	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	maxInflight = 8
	window      = 50
)

// ChunkRequest describes a single chunk fetch to dispatch.
type ChunkRequest struct {
	CID   string
	Index int
	Peer  peer.ID
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
}

// NewScheduler creates a Scheduler backed by the given Scorer.
func NewScheduler(scorer *scoring.Scorer) *Scheduler {
	return &Scheduler{
		scorer:     scorer,
		chunkPeers: make(map[int][]peer.ID),
		inflight:   make(map[int]struct{}),
		completed:  make(map[int]struct{}),
		failed:     make(map[int]map[peer.ID]struct{}),
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

// NextRequests returns up to maxInflight new ChunkRequests to dispatch.
// Prioritisation: chunks closer to current position first; break ties by rarity (fewer peers).
func (s *Scheduler) NextRequests() []ChunkRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	available := maxInflight - len(s.inflight)
	if available <= 0 {
		return nil
	}

	type candidate struct {
		index    int
		peers    []peer.ID
		distance int
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
		// Filter out peers that have already failed this chunk
		var viable []peer.ID
		for _, p := range peers {
			if _, bad := s.failed[idx][p]; !bad {
				viable = append(viable, p)
			}
		}
		if len(viable) == 0 {
			continue
		}
		candidates = append(candidates, candidate{
			index:    idx,
			peers:    viable,
			distance: idx - s.currentPos,
		})
	}

	// Sort: closer distance first, then fewer peers (rarity)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		return len(candidates[i].peers) < len(candidates[j].peers)
	})

	var requests []ChunkRequest
	for _, c := range candidates {
		if len(requests) >= available {
			break
		}
		best := s.scorer.BestPeers(1, c.peers)
		if len(best) == 0 {
			continue
		}
		requests = append(requests, ChunkRequest{
			CID:   s.cid,
			Index: c.index,
			Peer:  best[0],
		})
		s.inflight[c.index] = struct{}{}
	}
	return requests
}

// MarkCompleted removes a chunk from inflight and records it as done.
func (s *Scheduler) MarkCompleted(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, index)
	s.completed[index] = struct{}{}
}

// MarkFailed removes a chunk from inflight and records the peer failure.
func (s *Scheduler) MarkFailed(index int, p peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, index)
	if s.failed[index] == nil {
		s.failed[index] = make(map[peer.ID]struct{})
	}
	s.failed[index][p] = struct{}{}
}

// IsCompleted returns true if the chunk has been successfully downloaded.
func (s *Scheduler) IsCompleted(index int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.completed[index]
	return ok
}
