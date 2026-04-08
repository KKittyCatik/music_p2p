// Package queue provides a smart playback queue supporting history,
// dynamic next-track selection, manual overrides, and autoplay.
package queue

import (
	"sync"
)

// Item represents a single entry in the queue.
type Item struct {
	CID   string
	Title string
	Artist string
}

// Queue manages the ordered list of tracks to play, including history.
type Queue struct {
	mu      sync.Mutex
	items   []Item // upcoming items (index 0 = next to play)
	history []Item // previously played items (most-recent last)
	current *Item  // currently playing item, nil if idle
}

// New creates an empty Queue.
func New() *Queue {
	return &Queue{}
}

// Enqueue appends an item to the end of the queue.
func (q *Queue) Enqueue(item Item) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, item)
}

// Insert adds an item at position pos (0 = front / next to play).
// If pos is beyond the end, the item is appended.
func (q *Queue) Insert(pos int, item Item) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if pos < 0 {
		pos = 0
	}
	if pos >= len(q.items) {
		q.items = append(q.items, item)
		return
	}
	q.items = append(q.items, Item{}) // grow by one
	copy(q.items[pos+1:], q.items[pos:])
	q.items[pos] = item
}

// Next removes and returns the next item from the queue.
// If the queue is empty, ok is false.
// The item that was previously "current" is appended to history.
func (q *Queue) Next() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.current != nil {
		q.history = append(q.history, *q.current)
		q.current = nil
	}
	if len(q.items) == 0 {
		return Item{}, false
	}
	item := q.items[0]
	q.items = q.items[1:]
	q.current = &item
	return item, true
}

// Peek returns the next item without removing it.
func (q *Queue) Peek() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return Item{}, false
	}
	return q.items[0], true
}

// Current returns the item currently being played, if any.
func (q *Queue) Current() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.current == nil {
		return Item{}, false
	}
	return *q.current, true
}

// History returns a copy of the play history (oldest first).
func (q *Queue) History() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Item, len(q.history))
	copy(out, q.history)
	return out
}

// Upcoming returns a snapshot of the queued items.
func (q *Queue) Upcoming() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Item, len(q.items))
	copy(out, q.items)
	return out
}

// Len returns the number of items waiting in the queue.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Clear empties the upcoming queue without touching history.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = nil
}
