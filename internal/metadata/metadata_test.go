package metadata_test

import (
	"testing"
	"time"

	"github.com/KKittyCatik/music_p2p/internal/metadata"
	"github.com/stretchr/testify/assert"
)

func makeStore() *metadata.Store {
	return metadata.NewLocalStore()
}

func TestComputeMetaID(t *testing.T) {
	id := metadata.ComputeMetaID("Title", "Artist", 3*time.Minute)
	assert.NotEmpty(t, id)
	assert.Len(t, id, 64, "SHA-256 hex string is 64 chars")
}

func TestComputeMetaIDDeterministic(t *testing.T) {
	id1 := metadata.ComputeMetaID("Title", "Artist", 3*time.Minute)
	id2 := metadata.ComputeMetaID("Title", "Artist", 3*time.Minute)
	assert.Equal(t, id1, id2)
}

func TestComputeMetaIDDifferentInputs(t *testing.T) {
	id1 := metadata.ComputeMetaID("Title A", "Artist", 3*time.Minute)
	id2 := metadata.ComputeMetaID("Title B", "Artist", 3*time.Minute)
	assert.NotEqual(t, id1, id2)
}

func TestStoreAddLocal(t *testing.T) {
	s := makeStore()
	meta := metadata.TrackMetadata{
		CID:    "cid-1",
		Title:  "Song",
		Artist: "Band",
	}
	s.AddLocal(meta)

	got, ok := s.Get("cid-1")
	assert.True(t, ok)
	assert.Equal(t, "Song", got.Title)
}

func TestStoreSearch(t *testing.T) {
	s := makeStore()
	s.AddLocal(metadata.TrackMetadata{CID: "cid-1", Title: "Hello World", Artist: "Rock Band"})
	s.AddLocal(metadata.TrackMetadata{CID: "cid-2", Title: "Bye Bye", Artist: "Pop Group"})

	results := s.Search("hello")
	assert.Len(t, results, 1)
	assert.Equal(t, "cid-1", results[0].CID)
}

func TestStoreSearchCaseInsensitive(t *testing.T) {
	s := makeStore()
	s.AddLocal(metadata.TrackMetadata{CID: "cid-1", Title: "HELLO", Artist: "X"})
	results := s.Search("hello")
	assert.Len(t, results, 1)
}

func TestStoreSearchNoResults(t *testing.T) {
	s := makeStore()
	s.AddLocal(metadata.TrackMetadata{CID: "cid-1", Title: "Something", Artist: "Y"})
	results := s.Search("zzz-no-match")
	assert.Empty(t, results)
}

func TestStoreGet(t *testing.T) {
	s := makeStore()
	s.AddLocal(metadata.TrackMetadata{CID: "cid-x", Title: "Track X"})

	got, ok := s.Get("cid-x")
	assert.True(t, ok)
	assert.Equal(t, "cid-x", got.CID)

	_, ok = s.Get("nonexistent")
	assert.False(t, ok)
}

func TestStoreGetByMetaID(t *testing.T) {
	s := makeStore()
	dur := 3 * time.Minute
	meta := metadata.TrackMetadata{
		CID:      "cid-m",
		Title:    "Meta Track",
		Artist:   "Meta Artist",
		Duration: dur,
	}
	s.AddLocal(meta)

	metaID := metadata.ComputeMetaID("Meta Track", "Meta Artist", dur)
	got, ok := s.GetByMetaID(metaID)
	assert.True(t, ok)
	assert.Equal(t, "cid-m", got.CID)
}

func TestUpsertDeduplication(t *testing.T) {
	s := makeStore()
	dur := 2 * time.Minute

	// First entry: incomplete (no artist).
	s.AddLocal(metadata.TrackMetadata{
		CID:      "cid-dup",
		Title:    "Dup Track",
		Duration: dur,
	})
	// Second entry: more complete (has artist).
	s.AddLocal(metadata.TrackMetadata{
		CID:      "cid-dup",
		Title:    "Dup Track",
		Artist:   "Artist",
		Duration: dur,
	})

	metaID := metadata.ComputeMetaID("Dup Track", "Artist", dur)
	got, ok := s.GetByMetaID(metaID)
	assert.True(t, ok)
	assert.Equal(t, "Artist", got.Artist)
}

func TestCompleteness(t *testing.T) {
	s := makeStore()
	s.AddLocal(metadata.TrackMetadata{CID: "c1", Title: "T", Artist: "A", Duration: time.Minute})
	all := s.All()
	assert.Len(t, all, 1)
}
