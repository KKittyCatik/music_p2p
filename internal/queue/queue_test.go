package queue_test

import (
	"sync"
	"testing"

	"github.com/KKittyCatik/music_p2p/internal/queue"
	"github.com/stretchr/testify/assert"
)

var (
	itemA = queue.Item{CID: "cid-a", Title: "Track A", Artist: "Artist A"}
	itemB = queue.Item{CID: "cid-b", Title: "Track B", Artist: "Artist B"}
	itemC = queue.Item{CID: "cid-c", Title: "Track C", Artist: "Artist C"}
)

func TestEnqueue(t *testing.T) {
	q := queue.New()
	q.Enqueue(itemA)
	q.Enqueue(itemB)

	upcoming := q.Upcoming()
	assert.Equal(t, []queue.Item{itemA, itemB}, upcoming)
}

func TestNext(t *testing.T) {
	q := queue.New()
	q.Enqueue(itemA)
	q.Enqueue(itemB)

	got, ok := q.Next()
	assert.True(t, ok)
	assert.Equal(t, itemA, got)

	cur, ok := q.Current()
	assert.True(t, ok)
	assert.Equal(t, itemA, cur)

	assert.Equal(t, 1, q.Len())
}

func TestNextEmptyQueue(t *testing.T) {
	q := queue.New()
	_, ok := q.Next()
	assert.False(t, ok)
}

func TestInsert(t *testing.T) {
	q := queue.New()
	q.Enqueue(itemA)
	q.Enqueue(itemC)

	// Insert B between A and C.
	q.Insert(1, itemB)

	upcoming := q.Upcoming()
	assert.Equal(t, []queue.Item{itemA, itemB, itemC}, upcoming)
}

func TestInsertAtBeginning(t *testing.T) {
	q := queue.New()
	q.Enqueue(itemB)
	q.Insert(0, itemA)

	upcoming := q.Upcoming()
	assert.Equal(t, itemA, upcoming[0])
	assert.Equal(t, itemB, upcoming[1])
}

func TestInsertNegativePos(t *testing.T) {
	q := queue.New()
	q.Enqueue(itemB)
	q.Insert(-5, itemA) // should clamp to 0

	upcoming := q.Upcoming()
	assert.Equal(t, itemA, upcoming[0])
}

func TestInsertBeyondEnd(t *testing.T) {
	q := queue.New()
	q.Enqueue(itemA)
	q.Insert(100, itemB) // beyond end → append

	upcoming := q.Upcoming()
	assert.Equal(t, []queue.Item{itemA, itemB}, upcoming)
}

func TestPeek(t *testing.T) {
	q := queue.New()
	q.Enqueue(itemA)
	q.Enqueue(itemB)

	got, ok := q.Peek()
	assert.True(t, ok)
	assert.Equal(t, itemA, got)
	assert.Equal(t, 2, q.Len(), "peek must not remove item")
}

func TestCurrent(t *testing.T) {
	q := queue.New()
	_, ok := q.Current()
	assert.False(t, ok, "no current before first Next")

	q.Enqueue(itemA)
	q.Next() //nolint:errcheck

	cur, ok := q.Current()
	assert.True(t, ok)
	assert.Equal(t, itemA, cur)
}

func TestHistory(t *testing.T) {
	q := queue.New()
	q.Enqueue(itemA)
	q.Enqueue(itemB)

	q.Next() //nolint:errcheck
	q.Next() //nolint:errcheck

	h := q.History()
	assert.Equal(t, 1, len(h), "first Next sets current, second moves first to history")
	assert.Equal(t, itemA, h[0])
}

func TestClear(t *testing.T) {
	q := queue.New()
	q.Enqueue(itemA)
	q.Enqueue(itemB)
	q.Next() //nolint:errcheck

	q.Clear()

	assert.Equal(t, 0, q.Len())
	// History should be intact.
	assert.Equal(t, 0, len(q.History()))
}

func TestLen(t *testing.T) {
	q := queue.New()
	assert.Equal(t, 0, q.Len())
	q.Enqueue(itemA)
	assert.Equal(t, 1, q.Len())
	q.Enqueue(itemB)
	assert.Equal(t, 2, q.Len())
	q.Next() //nolint:errcheck
	assert.Equal(t, 1, q.Len())
}

func TestConcurrency(t *testing.T) {
	q := queue.New()
	const goroutines = 20
	var wg sync.WaitGroup

	// Concurrent enqueues.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			q.Enqueue(queue.Item{CID: string(rune('a' + n))})
		}(i)
	}
	wg.Wait()

	// Concurrent nexts.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.Next() //nolint:errcheck
		}()
	}
	wg.Wait()
}
