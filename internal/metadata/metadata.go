package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/KKittyCatik/music_p2p/internal/bitrate"
	"github.com/libp2p/go-libp2p/core/host"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

const metadataTopic = "music-metadata"

// TrackMetadata holds all metadata for a track.
type TrackMetadata struct {
	CID      string           `json:"cid"`
	Title    string           `json:"title"`
	Artist   string           `json:"artist"`
	Duration time.Duration    `json:"duration"`
	Variants []bitrate.Variant `json:"variants"`
}

// Store manages track metadata, distributes it via gossipsub, and provides local search.
type Store struct {
	mu    sync.RWMutex
	index map[string]TrackMetadata // keyed by CID

	ps    *pubsub.PubSub
	topic *pubsub.Topic
	sub   *pubsub.Subscription
}

// NewStore creates a Store, sets up gossipsub, and starts listening for peer metadata.
func NewStore(ctx context.Context, h host.Host) (*Store, error) {
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("new gossipsub: %w", err)
	}

	topic, err := ps.Join(metadataTopic)
	if err != nil {
		return nil, fmt.Errorf("join topic: %w", err)
	}

	sub, err := topic.Subscribe()
	if err != nil {
		return nil, fmt.Errorf("subscribe topic: %w", err)
	}

	s := &Store{
		index: make(map[string]TrackMetadata),
		ps:    ps,
		topic: topic,
		sub:   sub,
	}

	go s.receiveLoop(ctx)
	return s, nil
}

// receiveLoop reads metadata published by other peers and stores it locally.
func (s *Store) receiveLoop(ctx context.Context) {
	for {
		msg, err := s.sub.Next(ctx)
		if err != nil {
			return
		}
		var meta TrackMetadata
		if err := json.Unmarshal(msg.Data, &meta); err != nil {
			continue
		}
		s.mu.Lock()
		s.index[meta.CID] = meta
		s.mu.Unlock()
	}
}

// Publish marshals meta to JSON and publishes it on the gossipsub topic.
func (s *Store) Publish(meta TrackMetadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := s.topic.Publish(context.Background(), data); err != nil {
		return fmt.Errorf("publish metadata: %w", err)
	}
	return nil
}

// AddLocal adds a TrackMetadata directly to the local store without publishing.
func (s *Store) AddLocal(meta TrackMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index[meta.CID] = meta
}

// Search returns all tracks whose Title or Artist contain query (case-insensitive).
func (s *Store) Search(query string) []TrackMetadata {
	q := strings.ToLower(query)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []TrackMetadata
	for _, m := range s.index {
		if strings.Contains(strings.ToLower(m.Title), q) ||
			strings.Contains(strings.ToLower(m.Artist), q) {
			results = append(results, m)
		}
	}
	return results
}

// Get returns the TrackMetadata for a given CID.
func (s *Store) Get(cid string) (TrackMetadata, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.index[cid]
	return m, ok
}

// All returns every track in the local store.
func (s *Store) All() []TrackMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TrackMetadata, 0, len(s.index))
	for _, m := range s.index {
		out = append(out, m)
	}
	return out
}
