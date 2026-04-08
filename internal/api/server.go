// Package api exposes the music-p2p node over a REST HTTP API.
//
// @title Music P2P API
// @version 1.0
// @description P2P Music Streaming Node REST API
// @host localhost:8080
// @BasePath /api/v1
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	httpswagger "github.com/swaggo/http-swagger"

	_ "github.com/KKittyCatik/music_p2p/docs"
	"github.com/KKittyCatik/music_p2p/internal/bitrate"
	"github.com/KKittyCatik/music_p2p/internal/metadata"
	"github.com/KKittyCatik/music_p2p/internal/queue"
	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/KKittyCatik/music_p2p/internal/storage"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// HostInfo is the subset of libp2p host functionality used by the API.
// The concrete libp2p host.Host type satisfies this interface.
type HostInfo interface {
	ID() peer.ID
	Addrs() []ma.Multiaddr
	Network() network.Network
	Connect(ctx context.Context, pi peer.AddrInfo) error
}

// MetadataBackend is the subset of metadata.Store used by the API.
type MetadataBackend interface {
	All() []metadata.TrackMetadata
	Search(q string) []metadata.TrackMetadata
	Get(cid string) (metadata.TrackMetadata, bool)
	AddLocal(meta metadata.TrackMetadata)
	Publish(meta metadata.TrackMetadata) error
}

// EngineBackend is the subset of streaming.Engine used by the API.
type EngineBackend interface {
	StartStreaming(ctx context.Context, cid string) error
	Stop()
	Seek(chunkIndex int)
	AdaptiveBitrate() *bitrate.AdaptiveBitrate
}

// DHTBackend is the subset of dht.DHT used by the API.
type DHTBackend interface {
	Provide(ctx context.Context, cid string) error
	FindProviders(ctx context.Context, cid string) ([]peer.AddrInfo, error)
}

// Server is the HTTP API server for the music-p2p node.
type Server struct {
	router    *mux.Router
	httpSrv   *http.Server
	startTime time.Time

	// Core components – all optional (nil-safe handlers).
	stor     *storage.Storage
	meta     MetadataBackend
	q        *queue.Queue
	scorer   *scoring.Scorer
	engine   EngineBackend
	dht      DHTBackend
	hostInfo HostInfo

	// Playback state.
	playMu    sync.Mutex
	playingCID string
	isPlaying  bool
	chunkIndex int
}

// Config holds the dependencies injected into a Server.
type Config struct {
	Storage  *storage.Storage
	Metadata MetadataBackend
	Queue    *queue.Queue
	Scorer   *scoring.Scorer
	Engine   EngineBackend
	DHT      DHTBackend
	Host     HostInfo
}

// New creates a Server and wires up all routes.
func New(cfg Config) *Server {
	s := &Server{
		router:    mux.NewRouter(),
		startTime: time.Now(),
		stor:      cfg.Storage,
		meta:      cfg.Metadata,
		q:         cfg.Queue,
		scorer:    cfg.Scorer,
		engine:    cfg.Engine,
		dht:       cfg.DHT,
		hostInfo:  cfg.Host,
	}
	s.routes()
	return s
}

// routes registers all API routes.
func (s *Server) routes() {
	api := s.router.PathPrefix("/api/v1").Subrouter()
	api.Use(corsMiddleware, jsonMiddleware, loggingMiddleware, recoveryMiddleware)

	// Node status
	api.HandleFunc("/status", s.handleGetStatus).Methods(http.MethodGet)

	// Tracks / Storage
	api.HandleFunc("/tracks", s.handleGetTracks).Methods(http.MethodGet)
	api.HandleFunc("/tracks/share", s.handleShareTrack).Methods(http.MethodPost)
	api.HandleFunc("/tracks/{cid}", s.handleDeleteTrack).Methods(http.MethodDelete)

	// Metadata
	api.HandleFunc("/metadata", s.handleGetAllMetadata).Methods(http.MethodGet)
	api.HandleFunc("/metadata/search", s.handleSearchMetadata).Methods(http.MethodGet)
	api.HandleFunc("/metadata/{cid}", s.handleGetMetadata).Methods(http.MethodGet)
	api.HandleFunc("/metadata", s.handlePublishMetadata).Methods(http.MethodPost)

	// Playback
	api.HandleFunc("/playback/play", s.handlePlay).Methods(http.MethodPost)
	api.HandleFunc("/playback/stop", s.handleStop).Methods(http.MethodPost)
	api.HandleFunc("/playback/seek", s.handleSeek).Methods(http.MethodPost)
	api.HandleFunc("/playback/status", s.handlePlaybackStatus).Methods(http.MethodGet)

	// Queue
	api.HandleFunc("/queue", s.handleGetQueue).Methods(http.MethodGet)
	api.HandleFunc("/queue", s.handleEnqueue).Methods(http.MethodPost)
	api.HandleFunc("/queue/insert", s.handleInsertQueue).Methods(http.MethodPost)
	api.HandleFunc("/queue", s.handleClearQueue).Methods(http.MethodDelete)
	api.HandleFunc("/queue/history", s.handleGetHistory).Methods(http.MethodGet)

	// Peers
	api.HandleFunc("/peers", s.handleGetPeers).Methods(http.MethodGet)
	api.HandleFunc("/peers/connect", s.handleConnectPeer).Methods(http.MethodPost)
	api.HandleFunc("/peers/{peerID}/score", s.handleGetPeerScore).Methods(http.MethodGet)

	// DHT
	api.HandleFunc("/dht/provide/{cid}", s.handleDHTProvide).Methods(http.MethodPost)
	api.HandleFunc("/dht/providers/{cid}", s.handleDHTProviders).Methods(http.MethodGet)

	// Engine
	api.HandleFunc("/engine/status", s.handleEngineStatus).Methods(http.MethodGet)

	// Swagger UI – served without the JSON middleware so the UI assets work.
	s.router.PathPrefix("/swagger/").Handler(httpswagger.WrapHandler)
}

// Run starts the HTTP server on the given address (e.g. ":8080").
// It blocks until the server is stopped via context cancellation.
func (s *Server) Run(ctx context.Context, addr string) error {
	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}
	go func() {
		<-ctx.Done()
		s.httpSrv.Shutdown(context.Background()) //nolint:errcheck
	}()
	err := s.httpSrv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// ServeHTTP makes Server usable directly with httptest.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// writeJSON encodes v as JSON into w.
func writeJSON(w http.ResponseWriter, v interface{}) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// writeStatus sets the status code and writes a JSON response.
func writeStatus(w http.ResponseWriter, code int, v interface{}) {
	w.WriteHeader(code)
	writeJSON(w, v)
}
