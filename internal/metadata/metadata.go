package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/KKittyCatik/music_p2p/internal/bitrate"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

const metadataTopic = "music-metadata"

// TrackMetadata holds all metadata for a track.
type TrackMetadata struct {
	CID      string            `json:"cid"`
	MetaID   string            `json:"meta_id"` // SHA-256(title + artist + duration)
	Title    string            `json:"title"`
	Artist   string            `json:"artist"`
	Duration time.Duration     `json:"duration"`
	Variants []bitrate.Variant `json:"variants"`
}

// ComputeMetaID derives a stable MetaID = hex(SHA-256(title + artist + duration)).
func ComputeMetaID(title, artist string, duration time.Duration) string {
	h := sha256.New()
	h.Write([]byte(title))
	h.Write([]byte(artist))
	h.Write([]byte(duration.String()))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// signedEnvelope is the wire format for signed metadata.
type signedEnvelope struct {
	Metadata  TrackMetadata `json:"metadata"`
	Signature string        `json:"signature"` // base64(sign(JSON(Metadata)))
	PeerID    string        `json:"peer_id"`
}

// completeness counts how many optional fields are non-zero.
func completeness(m TrackMetadata) int {
	score := 0
	if m.Title != "" {
		score++
	}
	if m.Artist != "" {
		score++
	}
	if m.Duration != 0 {
		score++
	}
	if len(m.Variants) > 0 {
		score++
	}
	return score
}

// Store manages track metadata, distributes it via gossipsub, and provides local search.
type Store struct {
	mu    sync.RWMutex
	index map[string]TrackMetadata // keyed by CID
	byMeta map[string]TrackMetadata // keyed by MetaID

	host  host.Host
	ps    *pubsub.PubSub
	topic *pubsub.Topic
	sub   *pubsub.Subscription
}

// NewLocalStore creates a metadata Store that only maintains a local in-memory
// index, without gossipsub. Useful for testing and nodes that do not require
// metadata broadcast.
func NewLocalStore() *Store {
	return &Store{
		index:  make(map[string]TrackMetadata),
		byMeta: make(map[string]TrackMetadata),
	}
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
		index:  make(map[string]TrackMetadata),
		byMeta: make(map[string]TrackMetadata),
		host:   h,
		ps:     ps,
		topic:  topic,
		sub:    sub,
	}

	go s.receiveLoop(ctx)
	return s, nil
}

// sign marshals meta to JSON and signs it with the host's private key.
func (s *Store) sign(meta TrackMetadata) (signedEnvelope, error) {
	privKey := s.host.Peerstore().PrivKey(s.host.ID())
	if privKey == nil {
		return signedEnvelope{}, fmt.Errorf("no private key available for host %s", s.host.ID())
	}
	payload, err := json.Marshal(meta)
	if err != nil {
		return signedEnvelope{}, fmt.Errorf("marshal metadata for signing: %w", err)
	}
	sig, err := privKey.Sign(payload)
	if err != nil {
		return signedEnvelope{}, fmt.Errorf("sign metadata: %w", err)
	}
	return signedEnvelope{
		Metadata:  meta,
		Signature: base64.StdEncoding.EncodeToString(sig),
		PeerID:    s.host.ID().String(),
	}, nil
}

// verify checks the signature in env against the sender's public key.
func verify(env signedEnvelope) error {
	senderID, err := peer.Decode(env.PeerID)
	if err != nil {
		return fmt.Errorf("decode peer id: %w", err)
	}
	pubKey, err := senderID.ExtractPublicKey()
	if err != nil {
		// Reject messages whose public key cannot be extracted (e.g. RSA peer IDs
		// that do not embed the key).  Accepting them without verification would
		// allow unsigned metadata to bypass the security check.
		return fmt.Errorf("cannot extract public key for %s: %w", env.PeerID, err)
	}
	payload, err := json.Marshal(env.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata for verify: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	ok, err := pubKey.Verify(payload, sig)
	if err != nil {
		return fmt.Errorf("verify signature: %w", err)
	}
	if !ok {
		return fmt.Errorf("invalid signature from %s", env.PeerID)
	}
	return nil
}

// receiveLoop reads metadata published by other peers, verifies signatures,
// and stores or merges the entry.
func (s *Store) receiveLoop(ctx context.Context) {
	for {
		msg, err := s.sub.Next(ctx)
		if err != nil {
			return
		}
		var env signedEnvelope
		if err := json.Unmarshal(msg.Data, &env); err != nil {
			log.Printf("metadata: received invalid envelope: %v", err)
			continue
		}
		if env.Signature == "" || env.PeerID == "" {
			log.Printf("metadata: rejecting unsigned message from %s", msg.ReceivedFrom)
			continue
		}
		if err := verify(env); err != nil {
			log.Printf("metadata: rejecting invalid signature from %s: %v", env.PeerID, err)
			continue
		}
		meta := env.Metadata
		// Ensure MetaID is set.
		if meta.MetaID == "" {
			meta.MetaID = ComputeMetaID(meta.Title, meta.Artist, meta.Duration)
		}
		s.upsert(meta)
	}
}

// upsert stores meta, deduplicating by MetaID and keeping the most complete entry.
func (s *Store) upsert(meta TrackMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Index by CID.
	s.index[meta.CID] = meta

	// Deduplicate by MetaID.
	if meta.MetaID == "" {
		return
	}
	existing, ok := s.byMeta[meta.MetaID]
	if !ok || completeness(meta) > completeness(existing) {
		s.byMeta[meta.MetaID] = meta
	}
}

// Publish signs meta and publishes the signed envelope on the gossipsub topic.
// If the store was created without gossipsub (e.g. NewLocalStore), this is a no-op.
func (s *Store) Publish(meta TrackMetadata) error {
	if s.topic == nil || s.host == nil {
		return nil
	}
	if meta.MetaID == "" {
		meta.MetaID = ComputeMetaID(meta.Title, meta.Artist, meta.Duration)
	}
	env, err := s.sign(meta)
	if err != nil {
		return fmt.Errorf("sign metadata: %w", err)
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	if err := s.topic.Publish(context.Background(), data); err != nil {
		return fmt.Errorf("publish metadata: %w", err)
	}
	return nil
}

// AddLocal adds a TrackMetadata directly to the local store without publishing.
func (s *Store) AddLocal(meta TrackMetadata) {
	if meta.MetaID == "" {
		meta.MetaID = ComputeMetaID(meta.Title, meta.Artist, meta.Duration)
	}
	s.upsert(meta)
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

// GetByMetaID returns the TrackMetadata for a given MetaID.
func (s *Store) GetByMetaID(metaID string) (TrackMetadata, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.byMeta[metaID]
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
