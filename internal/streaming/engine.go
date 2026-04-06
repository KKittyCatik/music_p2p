package streaming

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/KKittyCatik/music_p2p/internal/dht"
	"github.com/KKittyCatik/music_p2p/internal/scheduler"
	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/KKittyCatik/music_p2p/internal/storage"
	p2phost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

const musicProtocol = "/music/1.0.0"

// Engine downloads chunks in parallel and exposes them as an io.Reader.
type Engine struct {
	host    p2phost.Host
	dht     *dht.DHT
	storage *storage.Storage
	scorer  *scoring.Scorer

	cancelFunc context.CancelFunc

	mu          sync.Mutex
	chunks      map[int][]byte // index → raw bytes
	nextRead    int            // next chunk index for Read()
	totalChunks int
	cid         string
	done        bool

	readCond *sync.Cond
}

// NewEngine constructs an Engine.
func NewEngine(h p2phost.Host, dhtNode *dht.DHT, stor *storage.Storage, sc *scoring.Scorer) *Engine {
	e := &Engine{
		host:    h,
		dht:     dhtNode,
		storage: stor,
		scorer:  sc,
		chunks:  make(map[int][]byte),
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

	// Connect to providers
	for _, p := range providers {
		if p.ID == e.host.ID() {
			continue
		}
		e.host.Peerstore().AddAddrs(p.ID, p.Addrs, time.Hour)
		if err := e.host.Connect(ctx, p); err != nil {
			// Log but continue – other providers may be reachable.
			_ = err
		}
	}

	// Query total chunks from the first reachable peer
	totalChunks, err := e.queryTotalChunks(ctx, cid, providers)
	if err != nil {
		return fmt.Errorf("query total chunks: %w", err)
	}

	e.mu.Lock()
	e.cid = cid
	e.totalChunks = totalChunks
	e.mu.Unlock()

	// Build chunk→peers map: for simplicity all providers advertise all chunks
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

	go e.downloadLoop(childCtx, sched, cid, totalChunks)
	return nil
}

// downloadLoop drives the scheduler and launches goroutines to fetch chunks.
func (e *Engine) downloadLoop(ctx context.Context, sched *scheduler.Scheduler, cid string, total int) {
	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		default:
		}

		requests := sched.NextRequests()
		if len(requests) == 0 {
			// Check if we are done
			e.mu.Lock()
			completed := len(e.chunks)
			e.mu.Unlock()
			if completed >= total {
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
				if err != nil {
					e.scorer.RecordFailure(r.Peer)
					sched.MarkFailed(r.Index, r.Peer)
					return
				}
				e.scorer.RecordSuccess(r.Peer, time.Since(start), len(data))
				sched.MarkCompleted(r.Index)

				e.mu.Lock()
				e.chunks[r.Index] = data
				e.mu.Unlock()
				e.readCond.Broadcast()

				// Advance playback window
				e.mu.Lock()
				pos := e.nextRead
				e.mu.Unlock()
				sched.SetCurrentPosition(pos)
			}(req)
		}
	}
}

// fetchChunk sends "GET <cid> <index>\n" to the peer and reads the response.
func (e *Engine) fetchChunk(ctx context.Context, peerID peer.ID, cid string, index int) ([]byte, error) {
	s, err := e.host.NewStream(ctx, peerID, musicProtocol)
	if err != nil {
		return nil, fmt.Errorf("new stream to %s: %w", peerID, err)
	}
	defer s.Close()

	fmt.Fprintf(s, "GET %s %d\n", cid, index)

	reader := bufio.NewReader(s)
	// Peek to check for error
	peek, err := reader.Peek(3)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("peek response: %w", err)
	}
	if string(peek) == "ERR" {
		return nil, fmt.Errorf("peer returned ERR for chunk %d", index)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read chunk data: %w", err)
	}
	return data, nil
}

// queryTotalChunks asks a provider how many chunks the track has by sending
// "GET <cid> -1\n" as a metadata request; on ERR it falls back to checking
// the local storage if available, then defaults to 1.
func (e *Engine) queryTotalChunks(ctx context.Context, cid string, providers []peer.AddrInfo) (int, error) {
	// Check local storage first
	if n := e.storage.GetTotalChunks(cid); n > 0 {
		return n, nil
	}

	// Ask first reachable provider using a TOTAL <cid> protocol message
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

// Stop cancels the download context.
func (e *Engine) Stop() {
	if e.cancelFunc != nil {
		e.cancelFunc()
	}
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
				// Partial read: trim consumed bytes and leave remainder
				e.chunks[e.nextRead] = data[n:]
				return n, nil
			}
			delete(e.chunks, e.nextRead)
			e.nextRead++
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
func ChunkBytes(c storage.Chunk) []byte {
	var buf bytes.Buffer
	for _, f := range c.Frames {
		buf.Write(f)
	}
	return buf.Bytes()
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
