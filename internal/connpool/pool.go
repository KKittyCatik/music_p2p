// Package connpool provides a per-peer libp2p stream connection pool that
// reuses open streams instead of opening a new stream for every request.
package connpool

import (
	"context"
	"sync"
	"time"

	"github.com/KKittyCatik/music_p2p/internal/metrics"
	p2phost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const (
	// maxIdleTimeout is how long an idle stream stays in the pool before being closed.
	maxIdleTimeout = 30 * time.Second
	// maxStreamsPerPeer is the maximum number of concurrent streams to the same peer.
	maxStreamsPerPeer = 8
)

// idleStream wraps a stream together with the time it was last returned.
type idleStream struct {
	stream   network.Stream
	idleSince time.Time
}

// peerPool holds idle streams and active stream count for one peer.
type peerPool struct {
	mu      sync.Mutex
	idle    []idleStream
	active  int
}

// Pool is a connection pool that reuses libp2p streams per peer.
type Pool struct {
	host     p2phost.Host
	protocol protocol.ID

	mu    sync.Mutex
	peers map[peer.ID]*peerPool

	closeOnce sync.Once
	done      chan struct{}
}

// New creates a new Pool for the given host and protocol.
func New(h p2phost.Host, proto protocol.ID) *Pool {
	p := &Pool{
		host:     h,
		protocol: proto,
		peers:    make(map[peer.ID]*peerPool),
		done:     make(chan struct{}),
	}
	go p.reapLoop()
	return p
}

// Close stops the background reap goroutine and closes all idle streams.
func (p *Pool) Close() {
	p.closeOnce.Do(func() { close(p.done) })
}

// getOrCreatePeer returns the peerPool for the given peer, creating it if needed.
func (p *Pool) getOrCreatePeer(id peer.ID) *peerPool {
	p.mu.Lock()
	defer p.mu.Unlock()
	pp, ok := p.peers[id]
	if !ok {
		pp = &peerPool{}
		p.peers[id] = pp
	}
	return pp
}

// Acquire returns an existing idle stream or opens a new one for peerID.
// The caller must call Release when done.
func (p *Pool) Acquire(ctx context.Context, peerID peer.ID) (network.Stream, error) {
	pp := p.getOrCreatePeer(peerID)

	pp.mu.Lock()
	// Try to reuse an idle stream.
	for len(pp.idle) > 0 {
		last := len(pp.idle) - 1
		is := pp.idle[last]
		pp.idle = pp.idle[:last]

		// Check if the stream is still usable (not expired).
		if time.Since(is.idleSince) < maxIdleTimeout {
			pp.active++
			pp.mu.Unlock()
			metrics.PoolActiveStreams.Inc()
			metrics.PoolIdleStreams.Dec()
			return is.stream, nil
		}
		// Expired – close it and try the next one.
		is.stream.Close()
	}
	pp.mu.Unlock()

	// Open a new stream.
	s, err := p.host.NewStream(ctx, peerID, p.protocol)
	if err != nil {
		return nil, err
	}
	pp.mu.Lock()
	pp.active++
	pp.mu.Unlock()
	metrics.PoolActiveStreams.Inc()
	return s, nil
}

// Release returns a stream to the pool. If the stream is in an error state it
// is closed and discarded instead.
func (p *Pool) Release(peerID peer.ID, s network.Stream, hadError bool) {
	pp := p.getOrCreatePeer(peerID)
	pp.mu.Lock()
	defer pp.mu.Unlock()

	if pp.active > 0 {
		pp.active--
	}

	if hadError || len(pp.idle) >= maxStreamsPerPeer {
		s.Close()
		metrics.PoolActiveStreams.Dec()
		return
	}
	pp.idle = append(pp.idle, idleStream{stream: s, idleSince: time.Now()})
	metrics.PoolActiveStreams.Dec()
	metrics.PoolIdleStreams.Inc()
}

// ActiveCount returns the number of streams currently in use for peerID.
func (p *Pool) ActiveCount(peerID peer.ID) int {
	p.mu.Lock()
	pp, ok := p.peers[peerID]
	p.mu.Unlock()
	if !ok {
		return 0
	}
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.active
}

// LeastLoadedPeer selects the peer from candidates with the fewest active streams.
func (p *Pool) LeastLoadedPeer(candidates []peer.ID) peer.ID {
	if len(candidates) == 0 {
		return ""
	}
	best := candidates[0]
	bestCount := p.ActiveCount(best)
	for _, id := range candidates[1:] {
		if c := p.ActiveCount(id); c < bestCount {
			best = id
			bestCount = c
		}
	}
	return best
}

// reapLoop periodically removes expired idle streams.
func (p *Pool) reapLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.mu.Lock()
			for _, pp := range p.peers {
				pp.mu.Lock()
				var keep []idleStream
				for _, is := range pp.idle {
					if time.Since(is.idleSince) < maxIdleTimeout {
						keep = append(keep, is)
					} else {
						is.stream.Close()
					}
				}
				pp.idle = keep
				pp.mu.Unlock()
			}
			p.mu.Unlock()
		}
	}
}
