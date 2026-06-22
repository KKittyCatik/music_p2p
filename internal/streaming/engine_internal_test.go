package streaming

import (
	"sync"
	"testing"
)

// newBareEngine builds an Engine with just enough state to exercise acceptChunk
// without any libp2p host, DHT, or network.
func newBareEngine() *Engine {
	e := &Engine{chunks: make(map[int][]byte)}
	e.readCond = sync.NewCond(&e.mu)
	return e
}

func TestAcceptChunkNew(t *testing.T) {
	e := newBareEngine()
	if !e.acceptChunk(0, []byte("a")) {
		t.Fatal("first chunk should be accepted")
	}
	if e.received != 1 {
		t.Fatalf("received = %d, want 1", e.received)
	}
}

func TestAcceptChunkDuplicateBuffered(t *testing.T) {
	e := newBareEngine()
	e.acceptChunk(3, []byte("x"))
	// Re-deliver the same index while it is still buffered.
	if e.acceptChunk(3, []byte("x")) {
		t.Fatal("buffered duplicate should be rejected")
	}
	if e.received != 1 {
		t.Fatalf("received = %d, want 1 (duplicate must not be counted)", e.received)
	}
}

func TestAcceptChunkDuplicateConsumed(t *testing.T) {
	e := newBareEngine()
	e.acceptChunk(0, []byte("a"))
	// Simulate the reader having consumed index 0.
	delete(e.chunks, 0)
	e.nextRead = 1
	// A re-dispatched, already-consumed chunk must be dropped — not re-buffered.
	if e.acceptChunk(0, []byte("a")) {
		t.Fatal("consumed duplicate should be rejected")
	}
	if _, ok := e.chunks[0]; ok {
		t.Fatal("consumed chunk must not be re-added to the buffer")
	}
	if e.received != 1 {
		t.Fatalf("received = %d, want 1", e.received)
	}
}

// TestAcceptChunkReachesTotalExactly verifies that with duplicate deliveries the
// received counter still lands exactly on the total, so `done` is not set while a
// chunk index is still missing.
func TestAcceptChunkReachesTotalExactly(t *testing.T) {
	e := newBareEngine()
	const total = 5
	// Deliver every index once, plus a flood of duplicates.
	for i := 0; i < total; i++ {
		e.acceptChunk(i, []byte{byte(i)})
		e.acceptChunk(i, []byte{byte(i)}) // duplicate
	}
	if e.received != total {
		t.Fatalf("received = %d, want %d", e.received, total)
	}
	for i := 0; i < total; i++ {
		if _, ok := e.chunks[i]; !ok {
			t.Fatalf("chunk %d missing from buffer", i)
		}
	}
}
