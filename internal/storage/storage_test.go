package storage_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KKittyCatik/music_p2p/internal/storage"
	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	s := storage.New(t.TempDir())
	assert.NotNil(t, s)
	assert.Empty(t, s.ListTracks())
}

func TestGetTotalChunksEmpty(t *testing.T) {
	s := storage.New(t.TempDir())
	assert.Equal(t, 0, s.GetTotalChunks("nonexistent-cid"))
}

func TestGetChunkNotFound(t *testing.T) {
	s := storage.New(t.TempDir())
	_, err := s.GetChunk("nonexistent-cid", 0)
	assert.Error(t, err)
}

func TestGetChunkOutOfRange(t *testing.T) {
	s := storage.New(t.TempDir())
	// Build a minimal valid MP3 file to load.
	data := buildMinimalMP3()
	path := writeTempFile(t, data)

	cid, err := s.LoadTrack(path)
	assert.NoError(t, err)

	n := s.GetTotalChunks(cid)
	_, err = s.GetChunk(cid, n) // exactly one past the end
	assert.Error(t, err)
}

func TestListTracksEmpty(t *testing.T) {
	s := storage.New(t.TempDir())
	tracks := s.ListTracks()
	assert.Empty(t, tracks)
}

func TestRemoveTrack(t *testing.T) {
	s := storage.New(t.TempDir())
	data := buildMinimalMP3()
	path := writeTempFile(t, data)

	cid, err := s.LoadTrack(path)
	assert.NoError(t, err)
	assert.Len(t, s.ListTracks(), 1)

	err = s.RemoveTrack(cid)
	assert.NoError(t, err)
	assert.Empty(t, s.ListTracks())
}

func TestRemoveTrackNotFound(t *testing.T) {
	s := storage.New(t.TempDir())
	err := s.RemoveTrack("does-not-exist")
	assert.Error(t, err)
}

func TestMp3FrameSize(t *testing.T) {
	tests := []struct {
		name    string
		header  []byte
		wantPos bool // whether LoadTrack succeeds (frame found)
	}{
		{
			name: "valid MPEG1 Layer3 128kbps 44100Hz",
			// 0xFF 0xFB = sync + MPEG1 Layer3
			// 0x90 = 128kbps (index 9) + 44100Hz (index 0) + no padding
			// 0x00 = channel mode
			header:  buildValidMP3Frame(),
			wantPos: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := storage.New(t.TempDir())
			path := writeTempFile(t, tt.header)
			_, err := s.LoadTrack(path)
			if tt.wantPos {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// ---- helpers ----

func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.mp3")
	assert.NoError(t, err)
	_, err = f.Write(data)
	assert.NoError(t, err)
	f.Close()
	return filepath.Clean(f.Name())
}

// buildMinimalMP3 returns bytes containing one valid MPEG1 Layer3 frame
// (128 kbps, 44100 Hz, no padding) so LoadTrack can parse it.
func buildMinimalMP3() []byte {
	frame := buildValidMP3Frame()
	return frame
}

// buildValidMP3Frame returns a complete MPEG1 Layer3 128kbps 44100Hz frame.
// Frame size = 144 * 128000 / 44100 + 0 = 417 bytes.
func buildValidMP3Frame() []byte {
	// Header bytes for MPEG1, Layer3, 128kbps, 44100Hz, stereo, no padding.
	// b0=0xFF b1=0xFB (sync + MPEG1 + Layer3 + no CRC)
	// b2=0x90 (128kbps index=9, 44100Hz index=0, no padding, private=0)
	// b3=0xC0 (joint stereo)
	const frameSize = 417
	buf := make([]byte, frameSize)
	buf[0] = 0xFF
	buf[1] = 0xFB
	buf[2] = 0x90
	buf[3] = 0xC0
	return buf
}
