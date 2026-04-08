package streaming_test

import (
	"testing"

	"github.com/KKittyCatik/music_p2p/internal/storage"
	"github.com/KKittyCatik/music_p2p/internal/streaming"
	"github.com/stretchr/testify/assert"
)

func TestChunkBytes(t *testing.T) {
	chunk := storage.Chunk{
		Index:  0,
		Frames: [][]byte{[]byte("frame1"), []byte("frame2"), []byte("frame3")},
	}
	got := streaming.ChunkBytes(chunk)
	assert.Equal(t, []byte("frame1frame2frame3"), got)
}

func TestChunkBytesEmpty(t *testing.T) {
	chunk := storage.Chunk{Index: 0, Frames: nil}
	got := streaming.ChunkBytes(chunk)
	assert.Empty(t, got)
}

func TestChunkBytesSingleFrame(t *testing.T) {
	data := []byte{0xFF, 0xFB, 0x90, 0x00, 0x01, 0x02}
	chunk := storage.Chunk{Index: 0, Frames: [][]byte{data}}
	got := streaming.ChunkBytes(chunk)
	assert.Equal(t, data, got)
}
