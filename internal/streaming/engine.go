package streaming

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KKittyCatik/music_p2p/internal/bitrate"
	"github.com/KKittyCatik/music_p2p/internal/connpool"
	"github.com/KKittyCatik/music_p2p/internal/dht"
	"github.com/KKittyCatik/music_p2p/internal/metrics"
	"github.com/KKittyCatik/music_p2p/internal/scheduler"
	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/KKittyCatik/music_p2p/internal/storage"
	p2phost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	musicProtocol = "/music/1.0.0"

	// maxBufferChunks is the maximum number of chunks buffered ahead of nextRead.
	maxBufferChunks = 50
	// resumeBufferChunks is the threshold below which requesting resumes.
	resumeBufferChunks = maxBufferChunks / 2

	// stallTimeout is the time without a successful chunk before panic mode activates.
	stallTimeout = 2 * time.Second
)

// bufPool reuses bytes.Buffer instances for ChunkBytes() to reduce allocations.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Engine downloads chunks in parallel and exposes them as an io.Reader.
type Engine struct {
	host    p2phost.Host
	dht     *dht.DHT
	storage *storage.Storage
	scorer  *scoring.Scorer
	abr     *bitrate.AdaptiveBitrate
	pool    *connpool.Pool

	cancelFunc context.CancelFunc

	mu          sync.Mutex
	chunks      map[int][]byte // index → raw bytes
	nextRead    int            // next chunk index for Read()
	received    int            // total chunks fetched (including already consumed)
	totalChunks int
	cid         string
	done        bool

	// Anti-stall: time of last successful chunk receipt.
	lastChunkTime time.Time

	readCond *sync.Cond
}

// NewEngine constructs an Engine.
func NewEngine(h p2phost.Host, dhtNode *dht.DHT, stor *storage.Storage, sc *scoring.Scorer) *Engine {
	e := &Engine{
		host:          h,
		dht:           dhtNode,
		storage:       stor,
		scorer:        sc,
		abr:           bitrate.NewAdaptiveBitrate(),
		pool:          connpool.New(h, musicProtocol),
		chunks:        make(map[int][]byte),
		lastChunkTime: time.Now(),
	}
	e.readCond = sync.NewCond(&e.mu)
	return e
}

// StartStreaming begins downloading the track identified by cid.
// If the track is already in local storage it is served directly without P2P.
func (e *Engine) StartStreaming(ctx context.Context, cid string) error {
	if n := e.storage.GetTotalChunks(cid); n > 0 {
		childCtx, cancel := context.WithCancel(ctx)
		e.mu.Lock()
		e.cid = cid
		e.totalChunks = n
		e.nextRead = 0
		e.received = 0
		e.done = false
		e.chunks = make(map[int][]byte)
		e.lastChunkTime = time.Now()
		e.cancelFunc = cancel
		e.mu.Unlock()
		go e.serveFromLocalStorage(childCtx, cid, n)
		return nil
	}

	providers, err := e.dht.FindProviders(ctx, cid)
	if err != nil {
		return fmt.Errorf("find providers: %w", err)
	}
	if len(providers) == 0 {
		return fmt.Errorf("no providers found for %s", cid)
	}

	// Connect to providers.
	for _, p := range providers {
		if p.ID == e.host.ID() {
			continue
		}
		e.host.Peerstore().AddAddrs(p.ID, p.Addrs, time.Hour)
		if err := e.host.Connect(ctx, p); err != nil {
			log.Printf("streaming: failed to connect to provider %s: %v", p.ID, err)
		}
	}

	// Query total chunks from the first reachable peer.
	totalChunks, err := e.queryTotalChunks(ctx, cid, providers)
	if err != nil {
		return fmt.Errorf("query total chunks: %w", err)
	}

	e.mu.Lock()
	e.cid = cid
	e.totalChunks = totalChunks
	e.nextRead = 0
	e.received = 0
	e.done = false
	e.chunks = make(map[int][]byte)
	e.lastChunkTime = time.Now()
	e.mu.Unlock()

	// Build chunk→peers map: all providers advertise all chunks.
	chunkPeers := make(map[int][]peer.ID)
	for i := 0; i < totalChunks; i++ {
		for _, p := range providers {
			if p.ID != e.host.ID() {
				chunkPeers[i] = append(chunkPeers[i], p.ID)
			}
		}
	}

	sched := scheduler.NewScheduler(e.scorer)
	sched.SetCID(cid)
	sched.SetTotalChunks(totalChunks)
	sched.SetCurrentPosition(0)
	sched.SetAvailableChunks(chunkPeers)

	childCtx, cancel := context.WithCancel(ctx)
	e.cancelFunc = cancel

	go e.downloadLoop(childCtx, sched, cid, totalChunks, providers)
	return nil
}

// bufferAhead returns the number of chunks buffered strictly ahead of nextRead.
// Must be called with e.mu held.
func (e *Engine) bufferAhead() int {
	count := 0
	for idx := range e.chunks {
		if idx >= e.nextRead {
			count++
		}
	}
	return count
}

// serveFromLocalStorage feeds chunks directly from in-memory storage into the
// read buffer, bypassing DHT and P2P entirely.
func (e *Engine) serveFromLocalStorage(ctx context.Context, cid string, total int) {
	for i := 0; i < total; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		chunk, err := e.storage.GetChunk(cid, i)
		if err != nil {
			log.Printf("streaming: local chunk %d missing for %s: %v", i, cid, err)
			break
		}
		data := ChunkBytes(chunk)

		e.mu.Lock()
		for e.bufferAhead() >= maxBufferChunks {
			e.readCond.Wait()
			if ctx.Err() != nil {
				e.mu.Unlock()
				return
			}
		}
		e.chunks[i] = data
		e.received++
		e.lastChunkTime = time.Now()
		e.mu.Unlock()
		e.readCond.Broadcast()
	}

	e.mu.Lock()
	e.done = true
	e.mu.Unlock()
	e.readCond.Broadcast()
}

// downloadLoop drives the scheduler and launches goroutines to fetch chunks.
func (e *Engine) downloadLoop(ctx context.Context, sched *scheduler.Scheduler, cid string, total int, providers []peer.AddrInfo) {
	var wg sync.WaitGroup

	// Anti-stall monitor runs in its own goroutine so that it continues to
	// fire even when the download loop is blocked on backpressure
	// (readCond.Wait). Without this separation, a full buffer combined with
	// a stalled consumer would prevent the stall ticker from being serviced.
	go func() {
		stallTicker := time.NewTicker(500 * time.Millisecond)
		defer stallTicker.Stop()
		inPanic := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-stallTicker.C:
				e.mu.Lock()
				since := time.Since(e.lastChunkTime)
				done := e.done
				pos := e.nextRead
				e.mu.Unlock()
				if !done && since > stallTimeout {
					if !inPanic {
						log.Printf("streaming: stall detected (no chunks for %s), entering panic mode", since.Round(time.Millisecond))
						inPanic = true
						metrics.StallEvents.Inc()
						// Downgrade bitrate via ABR.
						e.abr.MeasureBandwidth(0, stallTimeout)
					}
					// Re-request all in-window chunks from any available peer.
					sched.ResetTo(pos)
				} else if inPanic && since <= stallTimeout {
					log.Printf("streaming: exiting panic mode")
					inPanic = false
				}
			}
		}
	}()

	for {
		// Check for context cancellation before each iteration.
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		default:
		}

		// Backpressure: block when the buffer is full, waiting for Read() to consume.
		e.mu.Lock()
		for e.bufferAhead() >= maxBufferChunks {
			e.readCond.Wait()
		}
		e.mu.Unlock()

		requests := sched.NextRequests()
		if len(requests) == 0 {
			// Check if we are done: all chunks fetched.
			e.mu.Lock()
			allFetched := e.received >= total
			e.mu.Unlock()
			if allFetched {
				e.mu.Lock()
				e.done = true
				e.mu.Unlock()
				e.readCond.Broadcast()
				wg.Wait()
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}

		for _, req := range requests {
			wg.Add(1)
			go func(r scheduler.ChunkRequest) {
				defer wg.Done()
				start := time.Now()
				data, err := e.fetchChunk(ctx, r.Peer, r.CID, r.Index)
				elapsed := time.Since(start)
				if err != nil {
					e.scorer.RecordFailure(r.Peer)
					sched.MarkFailed(r.Index, r.Peer)
					metrics.ChunksFailed.Inc()
					return
				}
				e.scorer.RecordSuccess(r.Peer, elapsed, len(data))
				metrics.ChunksDownloaded.Inc()
				metrics.ChunkLatency.Observe(elapsed.Seconds())
				// Update ABR bandwidth estimate.
				e.abr.MeasureBandwidth(len(data), elapsed)
				sched.MarkCompleted(r.Index)

				if !e.acceptChunk(r.Index, data) {
					return // duplicate – already consumed or buffered
				}

				e.mu.Lock()
				pos := e.nextRead
				bufferLevel := e.bufferAhead()
				e.mu.Unlock()
				metrics.BufferLevel.Set(float64(bufferLevel))
				e.readCond.Broadcast()

				// Advance playback window.
				sched.SetCurrentPosition(pos)
			}(req)
		}
	}
}

// acceptChunk stores a freshly fetched chunk and reports whether it was new.
// Duplicates are dropped: a chunk is a duplicate if its index is below the read
// pointer (already consumed) or is already buffered. The anti-stall ResetTo
// clears the scheduler's inflight set, so a slow chunk can be re-dispatched and
// arrive twice; counting it again would inflate `received`, flip `done` early,
// and truncate the stream. Deduplicating here keeps `received` exact.
func (e *Engine) acceptChunk(index int, data []byte) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if index < e.nextRead {
		return false
	}
	if _, exists := e.chunks[index]; exists {
		return false
	}
	e.chunks[index] = data
	e.received++
	e.lastChunkTime = time.Now()
	return true
}

// fetchChunk sends "GET <cid> <index>\n" to the peer over a pooled stream and
// reads a length-framed response: "<len>\n" followed by exactly <len> raw bytes.
// A length of -1 signals a peer-side error. The persistent reader carried by the
// pooled stream lets the same stream serve many requests across reuse cycles.
func (e *Engine) fetchChunk(ctx context.Context, peerID peer.ID, cid string, index int) ([]byte, error) {
	ps, err := e.pool.Acquire(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("acquire stream to %s: %w", peerID, err)
	}

	hadError := false
	defer func() {
		e.pool.Release(peerID, ps, hadError)
	}()

	if _, err := fmt.Fprintf(ps, "GET %s %d\n", cid, index); err != nil {
		hadError = true
		return nil, fmt.Errorf("write request: %w", err)
	}

	lenLine, err := ps.Reader.ReadString('\n')
	if err != nil {
		hadError = true
		return nil, fmt.Errorf("read length: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(lenLine))
	if err != nil {
		hadError = true
		return nil, fmt.Errorf("parse length %q: %w", lenLine, err)
	}
	if n < 0 {
		hadError = true
		return nil, fmt.Errorf("peer returned error for chunk %d", index)
	}

	data := make([]byte, n)
	if _, err := io.ReadFull(ps.Reader, data); err != nil {
		hadError = true
		return nil, fmt.Errorf("read chunk data: %w", err)
	}
	return data, nil
}

// queryTotalChunks asks a provider how many chunks the track has.
func (e *Engine) queryTotalChunks(ctx context.Context, cid string, providers []peer.AddrInfo) (int, error) {
	// Check local storage first.
	if n := e.storage.GetTotalChunks(cid); n > 0 {
		return n, nil
	}

	for _, p := range providers {
		if p.ID == e.host.ID() {
			continue
		}
		total, err := e.queryTotalFromPeer(ctx, p.ID, cid)
		if err == nil && total > 0 {
			return total, nil
		}
	}
	return 0, fmt.Errorf("could not determine total chunks for %s from any provider", cid)
}

// queryTotalFromPeer sends "TOTAL <cid>\n" and expects "<n>\n" back.
func (e *Engine) queryTotalFromPeer(ctx context.Context, peerID peer.ID, cid string) (int, error) {
	s, err := e.host.NewStream(ctx, peerID, musicProtocol)
	if err != nil {
		return 0, err
	}
	defer s.Close()

	if _, err := fmt.Fprintf(s, "TOTAL %s\n", cid); err != nil {
		return 0, err
	}
	line, err := bufio.NewReader(s).ReadString('\n')
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		return 0, fmt.Errorf("parse total %q: %w", line, err)
	}
	return n, nil
}

// Stop cancels the download context and closes the connection pool.
func (e *Engine) Stop() {
	if e.cancelFunc != nil {
		e.cancelFunc()
	}
	e.pool.Close()
}

// Seek resets the engine to start streaming from chunkIndex.
// It clears the buffer, resets the read pointer, and wakes up any blocked Read().
func (e *Engine) Seek(chunkIndex int) {
	metrics.SeekEvents.Inc()
	e.mu.Lock()
	e.nextRead = chunkIndex
	// Clear all buffered chunks.
	e.chunks = make(map[int][]byte)
	e.done = false
	e.lastChunkTime = time.Now()
	e.mu.Unlock()
	e.readCond.Broadcast()
}

// Read implements io.Reader, returning buffered chunk data in order.
// It blocks until the next chunk is available and returns io.EOF when done.
func (e *Engine) Read(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for {
		if data, ok := e.chunks[e.nextRead]; ok {
			n := copy(p, data)
			if n < len(data) {
				// Partial read: trim consumed bytes and leave remainder.
				e.chunks[e.nextRead] = data[n:]
				return n, nil
			}
			delete(e.chunks, e.nextRead)
			e.nextRead++
			// Signal the download loop that buffer space is available.
			e.readCond.Broadcast()
			return n, nil
		}
		if e.done && e.nextRead >= e.totalChunks {
			return 0, io.EOF
		}
		if e.done {
			// Done but chunk still missing – audio data is unrecoverable.
			return 0, fmt.Errorf("chunk %d missing after download completed", e.nextRead)
		}
		e.readCond.Wait()
	}
}

// ChunkBytes returns the raw bytes for a single chunk by concatenating its frames.
// It uses a sync.Pool to avoid allocating a new buffer on every call.
func ChunkBytes(c storage.Chunk) []byte {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	for _, f := range c.Frames {
		buf.Write(f)
	}
	// Copy to a fresh slice; the pool buffer will be reused.
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result
}

// ServeStream handles the music protocol. It loops over multiple requests on a
// single stream so the peer's connection pool can reuse it. Replies are
// length-framed: for GET it writes "<len>\n" then exactly <len> bytes (or
// "-1\n" on error); for TOTAL it writes "<n>\n". The loop ends when the remote
// closes the stream (EOF). Register via host.SetStreamHandler(musicProtocol, e.ServeStream).
func (e *Engine) ServeStream(s network.Stream) {
	defer s.Close()

	reader := bufio.NewReader(s)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return // remote closed the stream or read error
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}

		switch {
		case fields[0] == "TOTAL" && len(fields) == 2:
			fmt.Fprintf(s, "%d\n", e.storage.GetTotalChunks(fields[1]))

		case fields[0] == "GET" && len(fields) == 3:
			index, err := strconv.Atoi(fields[2])
			if err != nil {
				fmt.Fprintf(s, "-1\n")
				continue
			}
			chunk, err := e.storage.GetChunk(fields[1], index)
			if err != nil {
				fmt.Fprintf(s, "-1\n")
				continue
			}
			data := ChunkBytes(chunk)
			fmt.Fprintf(s, "%d\n", len(data))
			s.Write(data)

		default:
			fmt.Fprintf(s, "-1\n")
		}
	}
}

// AdaptiveBitrate returns the engine's ABR module (for external wiring if needed).
func (e *Engine) AdaptiveBitrate() *bitrate.AdaptiveBitrate {
	return e.abr
}

// WaitForChunks blocks until at least n chunks have been received, or ctx is
// cancelled.  It is used to implement instant-playback: start audio only after
// a minimal initial buffer is ready.
func (e *Engine) WaitForChunks(ctx context.Context, n int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for {
		if e.received >= n || e.done {
			return nil
		}
		// Check context cancellation without holding the lock.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		e.readCond.Wait()
	}
}

// ReadCloser adapts an Engine to io.ReadCloser. Close stops the underlying
// download and releases the connection pool, making the engine suitable for
// streaming over a single HTTP response.
type ReadCloser struct {
	*Engine
}

// Close stops the engine's download loop and connection pool.
func (rc ReadCloser) Close() error {
	rc.Engine.Stop()
	return nil
}

// NewReadCloser wraps an already-started engine as an io.ReadCloser.
func NewReadCloser(e *Engine) ReadCloser { return ReadCloser{e} }

// Host returns the libp2p host used by this engine.
func (e *Engine) Host() p2phost.Host { return e.host }

// DHT returns the DHT used by this engine.
func (e *Engine) DHT() *dht.DHT { return e.dht }

// Storage returns the storage used by this engine.
func (e *Engine) Storage() *storage.Storage { return e.storage }

// Scorer returns the scorer used by this engine.
func (e *Engine) Scorer() *scoring.Scorer { return e.scorer }
