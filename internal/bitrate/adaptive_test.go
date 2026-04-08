package bitrate_test

import (
	"testing"
	"time"

	"github.com/KKittyCatik/music_p2p/internal/bitrate"
	"github.com/stretchr/testify/assert"
)

func makeABR() *bitrate.AdaptiveBitrate {
	return bitrate.NewAdaptiveBitrate()
}

func TestNewAdaptiveBitrate(t *testing.T) {
	ab := makeABR()
	assert.NotNil(t, ab)
	assert.Equal(t, 0, ab.CurrentBandwidth())
}

func TestMeasureBandwidth(t *testing.T) {
	ab := makeABR()
	// 1 MB in 1 second = 8 Mbps.
	ab.MeasureBandwidth(1_000_000, time.Second)
	bw := ab.CurrentBandwidth()
	assert.Equal(t, 8_000_000, bw)
}

func TestMeasureBandwidthEMA(t *testing.T) {
	ab := makeABR()
	ab.MeasureBandwidth(1_000_000, time.Second) // 8 Mbps
	ab.MeasureBandwidth(2_000_000, time.Second) // 16 Mbps

	// EMA: 8_000_000*0.7 + 16_000_000*0.3 = 5_600_000 + 4_800_000 = 10_400_000
	assert.Equal(t, 10_400_000, ab.CurrentBandwidth())
}

func TestMeasureBandwidthZeroDuration(t *testing.T) {
	ab := makeABR()
	ab.MeasureBandwidth(1_000_000, 0) // should be no-op
	assert.Equal(t, 0, ab.CurrentBandwidth())
}

func TestSelectVariant(t *testing.T) {
	ab := makeABR()
	// Bandwidth = 400 kbps.
	ab.MeasureBandwidth(50_000, time.Second) // 400_000 bps

	tv := bitrate.TrackVariants{
		TrackID: "track-1",
		Variants: []bitrate.Variant{
			{Quality: "128kbps", CID: "cid-128", Bitrate: 128_000},
			{Quality: "256kbps", CID: "cid-256", Bitrate: 256_000},
			{Quality: "320kbps", CID: "cid-320", Bitrate: 320_000},
		},
	}
	ab.RegisterTrack(tv)

	// 256kbps * 1.2 = 307200 ≤ 400000 but 320kbps * 1.2 = 384000 ≤ 400000
	v, err := ab.SelectVariant("track-1")
	assert.NoError(t, err)
	assert.Equal(t, "320kbps", v.Quality)
}

func TestSelectVariantFallback(t *testing.T) {
	ab := makeABR()
	// Zero bandwidth → fallback to lowest.
	tv := bitrate.TrackVariants{
		TrackID: "track-2",
		Variants: []bitrate.Variant{
			{Quality: "128kbps", CID: "cid-128", Bitrate: 128_000},
			{Quality: "320kbps", CID: "cid-320", Bitrate: 320_000},
		},
	}
	ab.RegisterTrack(tv)

	v, err := ab.SelectVariant("track-2")
	assert.NoError(t, err)
	assert.Equal(t, "128kbps", v.Quality)
}

func TestSelectVariantUnregistered(t *testing.T) {
	ab := makeABR()
	_, err := ab.SelectVariant("no-such-track")
	assert.Error(t, err)
}

func TestSelectVariantNoVariants(t *testing.T) {
	ab := makeABR()
	ab.RegisterTrack(bitrate.TrackVariants{TrackID: "empty"})
	_, err := ab.SelectVariant("empty")
	assert.Error(t, err)
}

func TestRegisterTrack(t *testing.T) {
	ab := makeABR()
	tv := bitrate.TrackVariants{
		TrackID:  "track-reg",
		Variants: []bitrate.Variant{{Quality: "128kbps", Bitrate: 128_000}},
	}
	ab.RegisterTrack(tv)
	// Successful registration means SelectVariant works.
	ab.MeasureBandwidth(200_000, time.Second) // 1.6 Mbps
	_, err := ab.SelectVariant("track-reg")
	assert.NoError(t, err)
}

func TestCurrentBandwidth(t *testing.T) {
	ab := makeABR()
	assert.Equal(t, 0, ab.CurrentBandwidth())
	ab.MeasureBandwidth(125_000, time.Second) // 1 Mbps
	assert.Equal(t, 1_000_000, ab.CurrentBandwidth())
}
