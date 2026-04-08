package audio

import (
	"io"
	"log"
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

const (
	// preloadThreshold is the remaining playback time below which the next
	// track is preloaded to achieve gapless playback.
	preloadThreshold = 2 * time.Second
	// instantPlaybackChunks is the minimum number of chunks required before
	// playback starts (≤ 0.5 s worth of data at ~128 kbps ≈ 2–3 chunks).
	InstantPlaybackChunks = 3
)

// gaplessStreamer is a beep.Streamer that plays the current track and
// transparently switches to the next one without stopping audio output.
type gaplessStreamer struct {
	mu      sync.Mutex
	current beep.StreamSeekCloser
	next    beep.StreamSeekCloser
	doneCh  chan struct{} // closed when current has finished naturally
	once    sync.Once    // ensures doneCh is closed once
}

// Stream implements beep.Streamer.  It drains the current stream and, when
// exhausted, replaces it with the pre-loaded next stream without a gap.
func (g *gaplessStreamer) Stream(samples [][2]float64) (int, bool) {
	g.mu.Lock()
	cur := g.current
	g.mu.Unlock()

	if cur == nil {
		return 0, false
	}

	n, ok := cur.Stream(samples)
	if !ok {
		// Current stream exhausted — try switching to next.
		g.mu.Lock()
		nxt := g.next
		g.next = nil
		g.current = nxt
		g.mu.Unlock()

		g.once.Do(func() { close(g.doneCh) })

		if nxt == nil {
			return 0, false
		}
		// Stream the first samples from the new track to fill the buffer.
		n, _ = nxt.Stream(samples)
		return n, true
	}
	return n, true
}

// Err implements beep.Streamer.
func (g *gaplessStreamer) Err() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.current != nil {
		return g.current.Err()
	}
	return nil
}

// Player decodes MP3 streams and plays them through the system audio output.
// It supports gapless playback by accepting a second "next" reader that is
// preloaded and seamlessly switched to when the current track nears its end.
type Player struct {
	mu       sync.Mutex
	playing  bool
	stopCh   chan struct{}
	stopOnce sync.Once
	streamer *gaplessStreamer
}

// NewPlayer creates a new Player.
func NewPlayer() *Player {
	return &Player{
		stopCh: make(chan struct{}),
	}
}

// Play starts decoding the MP3 data from reader and plays it asynchronously.
// Call SetNext to register the follow-on track before the current one ends.
func (p *Player) Play(reader io.Reader) error {
	stream, format, err := mp3.Decode(io.NopCloser(reader))
	if err != nil {
		return err
	}

	if err := speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10)); err != nil {
		stream.Close()
		return err
	}

	gs := &gaplessStreamer{
		current: stream,
		doneCh:  make(chan struct{}),
	}

	p.mu.Lock()
	p.playing = true
	stopCh := make(chan struct{})
	p.stopCh = stopCh
	p.stopOnce = sync.Once{}
	p.streamer = gs
	p.mu.Unlock()

	allDoneCh := make(chan struct{})
	speaker.Play(beep.Seq(gs, beep.Callback(func() {
		close(allDoneCh)
	})))

	go func() {
		defer func() {
			stream.Close()
			p.mu.Lock()
			p.playing = false
			p.mu.Unlock()
		}()

		select {
		case <-allDoneCh:
		case <-stopCh:
			speaker.Clear()
		}
	}()

	return nil
}

// SetNext registers the next track reader so it is preloaded for gapless
// transition.  Safe to call from any goroutine.
func (p *Player) SetNext(reader io.Reader) {
	p.mu.Lock()
	gs := p.streamer
	p.mu.Unlock()

	if gs == nil {
		return
	}

	nextStream, _, err := mp3.Decode(io.NopCloser(reader))
	if err != nil {
		log.Printf("player: failed to preload next track: %v", err)
		return
	}

	gs.mu.Lock()
	if gs.next != nil {
		gs.next.Close()
	}
	gs.next = nextStream
	gs.mu.Unlock()
}

// Stop halts playback immediately. Safe to call multiple times.
func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.playing {
		p.stopOnce.Do(func() { close(p.stopCh) })
	}
}

// IsPlaying returns true while audio is playing.
func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// CurrentTrackDone returns a channel that is closed when the current track
// finishes streaming (even if the next track has already taken over).
// Returns nil if not playing.
func (p *Player) CurrentTrackDone() <-chan struct{} {
	p.mu.Lock()
	gs := p.streamer
	p.mu.Unlock()
	if gs == nil {
		return nil
	}
	return gs.doneCh
}
