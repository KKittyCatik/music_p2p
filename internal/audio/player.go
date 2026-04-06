package audio

import (
	"io"
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

// Player decodes an MP3 stream and plays it through the system audio output.
type Player struct {
	mu        sync.Mutex
	playing   bool
	stopCh    chan struct{}
}

// NewPlayer creates a new Player.
func NewPlayer() *Player {
	return &Player{
		stopCh: make(chan struct{}),
	}
}

// Play starts decoding the MP3 data from reader and plays it asynchronously.
func (p *Player) Play(reader io.Reader) error {
	stream, format, err := mp3.Decode(io.NopCloser(reader))
	if err != nil {
		return err
	}

	if err := speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10)); err != nil {
		stream.Close()
		return err
	}

	p.mu.Lock()
	p.playing = true
	stopCh := make(chan struct{})
	p.stopCh = stopCh
	p.mu.Unlock()

	doneCh := make(chan struct{})

	speaker.Play(beep.Seq(stream, beep.Callback(func() {
		close(doneCh)
	})))

	go func() {
		defer func() {
			stream.Close()
			p.mu.Lock()
			p.playing = false
			p.mu.Unlock()
		}()

		select {
		case <-doneCh:
		case <-stopCh:
			speaker.Clear()
		}
	}()

	return nil
}

// Stop halts playback immediately.
func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.playing {
		close(p.stopCh)
	}
}

// IsPlaying returns true while audio is playing.
func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}
