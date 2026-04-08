package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
)

const framesPerChunk = 32

// Chunk holds a slice of raw MP3 frames.
type Chunk struct {
	Index  int
	Frames [][]byte
}

// Storage manages loaded tracks and their chunks in memory.
type Storage struct {
	baseDir string
	mu      sync.RWMutex
	// tracks maps CID → ordered list of chunks
	tracks map[string][]Chunk
}

// New returns a new Storage rooted at baseDir.
func New(baseDir string) *Storage {
	return &Storage{
		baseDir: baseDir,
		tracks:  make(map[string][]Chunk),
	}
}

// LoadTrack reads the MP3 at path, hashes it, splits into frame-aligned chunks,
// stores them in memory and returns the hex-encoded SHA-256 CID.
func (s *Storage) LoadTrack(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", path, err)
	}

	sum := sha256.Sum256(data)
	cidStr := hex.EncodeToString(sum[:])

	frames := extractMP3Frames(data)
	if len(frames) == 0 {
		return "", fmt.Errorf("no MP3 frames found in %q", path)
	}
	chunks := groupFrames(frames)

	s.mu.Lock()
	s.tracks[cidStr] = chunks
	s.mu.Unlock()

	return cidStr, nil
}

// GetChunk returns the chunk at the given index for the given CID.
func (s *Storage) GetChunk(cidStr string, index int) (Chunk, error) {
	s.mu.RLock()
	chunks, ok := s.tracks[cidStr]
	s.mu.RUnlock()
	if !ok {
		return Chunk{}, fmt.Errorf("track %q not found", cidStr)
	}
	if index < 0 || index >= len(chunks) {
		return Chunk{}, errors.New("chunk index out of range")
	}
	return chunks[index], nil
}

// GetTotalChunks returns the number of chunks for the given CID.
func (s *Storage) GetTotalChunks(cidStr string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tracks[cidStr])
}

// RemoveTrack removes a track and its chunks from local storage.
// Returns an error if the track is not found.
func (s *Storage) RemoveTrack(cidStr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tracks[cidStr]; !ok {
		return fmt.Errorf("track %q not found", cidStr)
	}
	delete(s.tracks, cidStr)
	return nil
}

// ListTracks returns the CIDs of all loaded tracks.
func (s *Storage) ListTracks() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.tracks))
	for cid := range s.tracks {
		out = append(out, cid)
	}
	return out
}

// extractMP3Frames scans data and returns each MP3 frame as a byte slice.
// An MP3 sync word is 0xFF followed by 0xE0–0xFF (MPEG-1/2/2.5 layer I/II/III).
// Returns nil if no valid frames are found.
func extractMP3Frames(data []byte) [][]byte {
	var frames [][]byte
	i := 0
	for i < len(data)-3 {
		if data[i] == 0xFF && (data[i+1]&0xE0) == 0xE0 {
			size := mp3FrameSize(data[i:])
			if size > 0 && i+size <= len(data) {
				frames = append(frames, data[i:i+size])
				i += size
				continue
			}
		}
		i++
	}
	return frames
}

// mp3FrameSize returns the byte length of an MP3 frame starting at buf[0],
// or 0 if the header is invalid.
func mp3FrameSize(buf []byte) int {
	if len(buf) < 4 {
		return 0
	}
	// Header bytes
	b1 := buf[1]
	b2 := buf[2]

	// MPEG version
	version := (b1 >> 3) & 0x03
	// Layer
	layer := (b1 >> 1) & 0x03
	// Bitrate index
	bitrateIdx := (b2 >> 4) & 0x0F
	// Sampling rate index
	sampleIdx := (b2 >> 2) & 0x03
	// Padding
	padding := int((b2 >> 1) & 0x01)

	if bitrateIdx == 0 || bitrateIdx == 15 {
		return 0
	}
	if sampleIdx == 3 {
		return 0
	}

	bitrates := map[uint8]map[uint8][]int{
		// MPEG1 (version==3)
		3: {
			// Layer III
			1: {0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
			// Layer II
			2: {0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0},
			// Layer I
			3: {0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0},
		},
	}
	sampleRates := map[uint8][]int{
		3: {44100, 48000, 32000},
		2: {22050, 24000, 16000},
		0: {11025, 12000, 8000},
	}

	br, ok := bitrates[version]
	if !ok {
		return 0
	}
	brl, ok := br[layer]
	if !ok {
		return 0
	}
	bitrate := brl[bitrateIdx] * 1000

	sr, ok := sampleRates[version]
	if !ok {
		return 0
	}
	sampleRate := sr[sampleIdx]

	if bitrate == 0 || sampleRate == 0 {
		return 0
	}

	// Layer I: 384 samples/frame, slot=4 bytes
	if layer == 3 {
		return (12*bitrate/sampleRate+padding)*4
	}
	// Layer II/III: 1152 samples/frame, slot=1 byte
	return 144*bitrate/sampleRate + padding
}

// groupFrames groups consecutive frames into chunks of framesPerChunk each.
func groupFrames(frames [][]byte) []Chunk {
	var chunks []Chunk
	for i := 0; i < len(frames); i += framesPerChunk {
		end := i + framesPerChunk
		if end > len(frames) {
			end = len(frames)
		}
		chunk := Chunk{
			Index:  len(chunks),
			Frames: frames[i:end],
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}
