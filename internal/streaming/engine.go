package streaming

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
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

// readBufPool reuses byte slices for fetchChunk reads.
var readBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
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

// StartStreaming begins downloading the track identified by cid from available peers.
func (e *Engine) StartStreaming(ctx context.Context, cid string) error {
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

				e.mu.Lock()
				e.chunks[r.Index] = data
				e.received++
				e.lastChunkTime = time.Now()
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

// fetchChunk sends "GET <cid> <index>\n" to the peer using a pooled stream.
func (e *Engine) fetchChunk(ctx context.Context, peerID peer.ID, cid string, index int) ([]byte, error) {
	s, err := e.pool.Acquire(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("acquire stream to %s: %w", peerID, err)
	}

	hadError := false
	defer func() {
		e.pool.Release(peerID, s, hadError)
	}()

	if _, err := fmt.Fprintf(s, "GET %s %d\n", cid, index); err != nil {
		hadError = true
		return nil, fmt.Errorf("write request: %w", err)
	}

	reader := bufio.NewReader(s)
	// Peek to check for error response.
	peek, err := reader.Peek(3)
	if err != nil && err != io.EOF {
		hadError = true
		return nil, fmt.Errorf("peek response: %w", err)
	}
	if string(peek) == "ERR" {
		hadError = true
		return nil, fmt.Errorf("peer returned ERR for chunk %d", index)
	}

	// Use a pooled buffer for reading.
	bufPtr := readBufPool.Get().(*[]byte)
	defer readBufPool.Put(bufPtr)

	var result []byte
	for {
		n, readErr := reader.Read(*bufPtr)
		if n > 0 {
			result = append(result, (*bufPtr)[:n]...)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			hadError = true
			return nil, fmt.Errorf("read chunk data: %w", readErr)
		}
	}
	return result, nil
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

	fmt.Fprintf(s, "TOTAL %s\n", cid)

	var n int
	_, err = fmt.Fscanf(s, "%d\n", &n)
	if err != nil {
		return 0, err
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

// ServeStream handles the music protocol stream for chunk/total requests.
// Register this via host.SetStreamHandler(musicProtocol, engine.ServeStream).
func (e *Engine) ServeStream(s network.Stream) {
	defer s.Close()

	scanner := bufio.NewScanner(s)
	if !scanner.Scan() {
		return
	}
	line := scanner.Text()

	var cmd, cidStr string
	var index int

	if n, _ := fmt.Sscanf(line, "TOTAL %s", &cidStr); n == 1 {
		total := e.storage.GetTotalChunks(cidStr)
		fmt.Fprintf(s, "%d\n", total)
		return
	}

	if n, _ := fmt.Sscanf(line, "GET %s %d", &cidStr, &index); n == 2 {
		cmd = "GET"
	}

	if cmd != "GET" {
		fmt.Fprintf(s, "ERR\n")
		return
	}

	chunk, err := e.storage.GetChunk(cidStr, index)
	if err != nil {
		fmt.Fprintf(s, "ERR\n")
		return
	}
	s.Write(ChunkBytes(chunk))
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

// Host returns the libp2p host used by this engine.
func (e *Engine) Host() p2phost.Host { return e.host }

// DHT returns the DHT used by this engine.
func (e *Engine) DHT() *dht.DHT { return e.dht }

// Storage returns the storage used by this engine.
func (e *Engine) Storage() *storage.Storage { return e.storage }

// Scorer returns the scorer used by this engine.
func (e *Engine) Scorer() *scoring.Scorer { return e.scorer }
