package scoring

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type peerStats struct {
	successes   int
	failures    int
	totalBytes  int
	totalTimeMs float64 // milliseconds of successful transfers
}

// Scorer tracks per-peer performance metrics and computes a composite score.
type Scorer struct {
	mu    sync.RWMutex
	stats map[peer.ID]*peerStats
}

// NewScorer returns a new Scorer.
func NewScorer() *Scorer {
	return &Scorer{stats: make(map[peer.ID]*peerStats)}
}

// RecordSuccess records a successful chunk transfer.
func (s *Scorer) RecordSuccess(peerID peer.ID, latency time.Duration, bytes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps := s.getOrCreate(peerID)
	ps.successes++
	ps.totalBytes += bytes
	ps.totalTimeMs += float64(latency.Milliseconds())
}

// RecordFailure records a failed chunk transfer.
func (s *Scorer) RecordFailure(peerID peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getOrCreate(peerID).failures++
}

// Score returns the composite score for a peer (higher is better).
// Formula: (successRate * 0.4) + (throughputScore * 0.3) + (latencyScore * 0.3)
func (s *Scorer) Score(peerID peer.ID) float64 {
	s.mu.RLock()
	ps, ok := s.stats[peerID]
	s.mu.RUnlock()
	if !ok {
		return 0.5 // neutral score for unknown peers
	}

	total := ps.successes + ps.failures
	var successRate float64
	if total > 0 {
		successRate = float64(ps.successes) / float64(total)
	}

	var latencyScore float64
	if ps.successes > 0 {
		avgLatencyMs := ps.totalTimeMs / float64(ps.successes)
		latencyScore = 1.0 / (1.0 + avgLatencyMs/100.0)
	}

	var throughputScore float64
	if ps.totalTimeMs > 0 {
		bytesPerSec := float64(ps.totalBytes) / (ps.totalTimeMs / 1000.0)
		throughputScore = math.Min(1.0, bytesPerSec/1_000_000.0)
	}

	return successRate*0.4 + throughputScore*0.3 + latencyScore*0.3
}

// BestPeers returns up to n candidates sorted by descending score.
func (s *Scorer) BestPeers(n int, candidates []peer.ID) []peer.ID {
	sorted := make([]peer.ID, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool {
		return s.Score(sorted[i]) > s.Score(sorted[j])
	})
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

func (s *Scorer) getOrCreate(id peer.ID) *peerStats {
	ps, ok := s.stats[id]
	if !ok {
		ps = &peerStats{}
		s.stats[id] = ps
	}
	return ps
}
