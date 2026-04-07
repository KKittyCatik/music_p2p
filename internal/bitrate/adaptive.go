package bitrate

import (
	"errors"
	"sync"
	"time"
)

// Variant describes a single quality level for a track.
type Variant struct {
	Quality string // e.g. "128kbps", "256kbps", "320kbps"
	CID     string
	Bitrate int // bits per second
}

// TrackVariants holds all quality variants for one track.
type TrackVariants struct {
	TrackID  string
	Variants []Variant
}

// AdaptiveBitrate selects the best quality variant given current network conditions.
type AdaptiveBitrate struct {
	mu            sync.RWMutex
	tracks        map[string]TrackVariants
	bandwidthBits int // current measured bandwidth in bits/sec
}

// NewAdaptiveBitrate creates a new AdaptiveBitrate manager.
func NewAdaptiveBitrate() *AdaptiveBitrate {
	return &AdaptiveBitrate{
		tracks: make(map[string]TrackVariants),
	}
}

// RegisterTrack stores the variant list for a track.
func (ab *AdaptiveBitrate) RegisterTrack(tv TrackVariants) {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	ab.tracks[tv.TrackID] = tv
}

// MeasureBandwidth updates the estimated bandwidth based on a recent transfer observation.
func (ab *AdaptiveBitrate) MeasureBandwidth(bytesReceived int, duration time.Duration) {
	if duration <= 0 {
		return
	}
	bitsPerSec := int(float64(bytesReceived*8) / duration.Seconds())
	ab.mu.Lock()
	defer ab.mu.Unlock()
	// Exponential moving average (α = 0.3)
	if ab.bandwidthBits == 0 {
		ab.bandwidthBits = bitsPerSec
	} else {
		ab.bandwidthBits = int(float64(ab.bandwidthBits)*0.7 + float64(bitsPerSec)*0.3)
	}
}

// SelectVariant returns the highest quality variant whose bitrate fits within
// the current bandwidth (with 20% headroom: bandwidth >= bitrate * 1.2).
func (ab *AdaptiveBitrate) SelectVariant(trackID string) (Variant, error) {
	ab.mu.RLock()
	defer ab.mu.RUnlock()

	tv, ok := ab.tracks[trackID]
	if !ok {
		return Variant{}, errors.New("track not registered")
	}

	var best *Variant
	for i := range tv.Variants {
		v := &tv.Variants[i]
		required := int(float64(v.Bitrate) * 1.2)
		if ab.bandwidthBits >= required {
			if best == nil || v.Bitrate > best.Bitrate {
				best = v
			}
		}
	}
	if best == nil {
		// Fall back to lowest bitrate variant
		if len(tv.Variants) == 0 {
			return Variant{}, errors.New("no variants available")
		}
		lowest := tv.Variants[0]
		for _, v := range tv.Variants[1:] {
			if v.Bitrate < lowest.Bitrate {
				lowest = v
			}
		}
		return lowest, nil
	}
	return *best, nil
}

// CurrentBandwidth returns the latest measured bandwidth estimate in bits/sec.
func (ab *AdaptiveBitrate) CurrentBandwidth() int {
	ab.mu.RLock()
	defer ab.mu.RUnlock()
	return ab.bandwidthBits
}
