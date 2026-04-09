// Package metrics registers Prometheus metrics for the music_p2p node.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// P2P
	PeersConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "music_p2p_peers_connected",
		Help: "Current number of connected peers.",
	})
	PeersTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "music_p2p_peers_total",
		Help: "Total all-time peer connections.",
	})

	// Streaming
	ChunksDownloaded = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "music_p2p_chunks_downloaded_total",
		Help: "Total number of chunks successfully downloaded.",
	})
	ChunksFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "music_p2p_chunks_failed_total",
		Help: "Total number of chunk download failures.",
	})
	ChunkLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "music_p2p_chunk_latency_seconds",
		Help:    "Latency of individual chunk downloads in seconds.",
		Buckets: prometheus.DefBuckets,
	})
	BufferLevel = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "music_p2p_buffer_level_chunks",
		Help: "Number of chunks currently buffered ahead of the read position.",
	})
	StreamsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "music_p2p_streams_active",
		Help: "Number of active streaming sessions.",
	})

	// Playback
	TracksPlayed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "music_p2p_tracks_played_total",
		Help: "Total number of tracks started for playback.",
	})
	PlaybackState = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "music_p2p_playback_state",
		Help: "Current playback state: 0 = stopped, 1 = playing.",
	})

	// API
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "music_p2p_http_requests_total",
			Help: "Total HTTP requests by method, path, and status.",
		},
		[]string{"method", "path", "status"},
	)
	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "music_p2p_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds by method and path.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// Engine
	BandwidthBps = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "music_p2p_bandwidth_bps",
		Help: "Current estimated download bandwidth in bytes per second.",
	})
	StallEvents = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "music_p2p_stall_events_total",
		Help: "Total number of streaming stall events detected.",
	})
	SeekEvents = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "music_p2p_seek_events_total",
		Help: "Total number of seek operations performed.",
	})

	// DHT
	DHTProvides = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "music_p2p_dht_provides_total",
		Help: "Total number of DHT provide announcements.",
	})
	DHTLookups = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "music_p2p_dht_lookups_total",
		Help: "Total number of DHT provider lookups.",
	})

	// Connection Pool
	PoolActiveStreams = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "music_p2p_pool_active_streams",
		Help: "Number of currently active (in-use) connection pool streams.",
	})
	PoolIdleStreams = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "music_p2p_pool_idle_streams",
		Help: "Number of currently idle connection pool streams.",
	})
)

func init() {
	prometheus.MustRegister(
		PeersConnected,
		PeersTotal,
		ChunksDownloaded,
		ChunksFailed,
		ChunkLatency,
		BufferLevel,
		StreamsActive,
		TracksPlayed,
		PlaybackState,
		HTTPRequestsTotal,
		HTTPRequestDuration,
		BandwidthBps,
		StallEvents,
		SeekEvents,
		DHTProvides,
		DHTLookups,
		PoolActiveStreams,
		PoolIdleStreams,
	)
}

// StartMetricsServer starts an HTTP server exposing the /metrics endpoint on addr.
func StartMetricsServer(addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(addr, mux)
}
