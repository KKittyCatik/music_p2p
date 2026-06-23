package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	"github.com/KKittyCatik/music_p2p/internal/api"
	"github.com/KKittyCatik/music_p2p/internal/bitrate"
	"github.com/KKittyCatik/music_p2p/internal/metadata"
	"github.com/KKittyCatik/music_p2p/internal/metrics"
	"github.com/KKittyCatik/music_p2p/internal/queue"
	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/KKittyCatik/music_p2p/internal/storage"
)

// spyEngine is an api.EngineBackend that records calls to the download-driving
// methods. On a headless node the /playback/* control plane is state-only, so
// these must never be invoked — calling StartStreaming with no consumer is what
// caused the buffer-fill, anti-stall "stall detected" spam, and libp2p stream
// leak that this change fixes.
type spyEngine struct {
	startCalls int32
	stopCalls  int32
	seekCalls  int32
}

func (e *spyEngine) StartStreaming(ctx context.Context, cid string) error {
	atomic.AddInt32(&e.startCalls, 1)
	return nil
}
func (e *spyEngine) Stop()        { atomic.AddInt32(&e.stopCalls, 1) }
func (e *spyEngine) Seek(idx int) { atomic.AddInt32(&e.seekCalls, 1) }
func (e *spyEngine) AdaptiveBitrate() *bitrate.AdaptiveBitrate {
	return bitrate.NewAdaptiveBitrate()
}

// newSpyServer builds an API server wired to a spyEngine so tests can assert the
// playback control plane never drives the shared engine.
func newSpyServer(t *testing.T) (*api.Server, *spyEngine) {
	t.Helper()
	eng := &spyEngine{}
	srv := api.New(api.Config{
		Storage:  storage.New(t.TempDir()),
		Metadata: metadata.NewLocalStore(),
		Queue:    queue.New(),
		Scorer:   scoring.NewScorer(),
		Engine:   eng,
	})
	return srv, eng
}

// newTestServer creates an API server with lightweight real components.
func newTestServer(t *testing.T) *api.Server {
	t.Helper()
	return api.New(api.Config{
		Storage:  storage.New(t.TempDir()),
		Metadata: metadata.NewLocalStore(),
		Queue:    queue.New(),
		Scorer:   scoring.NewScorer(),
	})
}

func doRequest(t *testing.T, srv *api.Server, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		assert.NoError(t, err)
		bodyBytes = b
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

func decodeResponse(t *testing.T, rr *httptest.ResponseRecorder) api.Response {
	t.Helper()
	var resp api.Response
	err := json.NewDecoder(rr.Body).Decode(&resp)
	assert.NoError(t, err)
	return resp
}

func TestGetStatus(t *testing.T) {
	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/status", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

func TestGetTracks(t *testing.T) {
	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/tracks", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

func TestGetMetadata(t *testing.T) {
	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/metadata", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

func TestSearchMetadata(t *testing.T) {
	meta := metadata.NewLocalStore()
	meta.AddLocal(metadata.TrackMetadata{CID: "abc123", Title: "Hello World", Artist: "Test"})

	srv := api.New(api.Config{
		Storage:  storage.New(t.TempDir()),
		Metadata: meta,
		Queue:    queue.New(),
		Scorer:   scoring.NewScorer(),
	})

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/metadata/search?q=hello", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)

	data, ok := resp.Data.([]interface{})
	assert.True(t, ok)
	assert.Len(t, data, 1)
}

func TestGetQueue(t *testing.T) {
	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/queue", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

func TestPostQueueEnqueue(t *testing.T) {
	srv := newTestServer(t)
	body := api.QueueItemRequest{CID: "cid-1", Title: "T", Artist: "A"}
	rr := doRequest(t, srv, http.MethodPost, "/api/v1/queue", body)
	assert.Equal(t, http.StatusCreated, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

func TestPostQueueEnqueueMissingCID(t *testing.T) {
	srv := newTestServer(t)
	body := api.QueueItemRequest{Title: "T"}
	rr := doRequest(t, srv, http.MethodPost, "/api/v1/queue", body)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPostQueueInsert(t *testing.T) {
	srv := newTestServer(t)
	pos := 0
	body := api.QueueItemRequest{CID: "cid-1", Title: "T", Artist: "A", Position: &pos}
	rr := doRequest(t, srv, http.MethodPost, "/api/v1/queue/insert", body)
	assert.Equal(t, http.StatusCreated, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

func TestDeleteQueue(t *testing.T) {
	srv := newTestServer(t)
	doRequest(t, srv, http.MethodPost, "/api/v1/queue",
		api.QueueItemRequest{CID: "cid-1"})

	rr := doRequest(t, srv, http.MethodDelete, "/api/v1/queue", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

func TestGetQueueHistory(t *testing.T) {
	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/queue/history", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

func TestGetPeers(t *testing.T) {
	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/peers", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

func TestGetEngineStatus(t *testing.T) {
	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/engine/status", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

func TestInvalidEndpoint(t *testing.T) {
	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/nonexistent", nil)
	// gorilla/mux returns 404 for unknown paths.
	assert.True(t, rr.Code == http.StatusNotFound || rr.Code == http.StatusMethodNotAllowed)
}

func TestMiddlewareContentType(t *testing.T) {
	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/status", nil)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
}

func TestStreamTrackSuccess(t *testing.T) {
	const audio = "fake-mp3-audio-bytes-0123456789"
	srv := api.New(api.Config{
		Storage:  storage.New(t.TempDir()),
		Metadata: metadata.NewLocalStore(),
		Queue:    queue.New(),
		Scorer:   scoring.NewScorer(),
		NewStream: func(ctx context.Context, cid string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(audio)), nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/abc123/stream", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "audio/mpeg", rr.Header().Get("Content-Type"))
	assert.Equal(t, audio, rr.Body.String())
}

func TestStreamTrackInvalidCID(t *testing.T) {
	srv := newTestServer(t)
	// Uppercase letters are not valid lowercase hex – expect 400.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/NOTHEX/stream", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestStreamTrackUnavailable(t *testing.T) {
	srv := newTestServer(t) // no NewStream wired
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/abc123/stream", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestShareTrackMissingFile(t *testing.T) {
	srv := newTestServer(t)

	// Send a multipart form with no "file" field – expect 400.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/share", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	resp := decodeResponse(t, rr)
	assert.False(t, resp.Success)
}

func TestShareTrackWithFile(t *testing.T) {
	srv := newTestServer(t)

	// Build a minimal valid MP3 multipart form (dummy bytes – storage will chunk it).
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "test.mp3")
	assert.NoError(t, err)
	// Write enough dummy bytes to produce at least one chunk.
	fw.Write(bytes.Repeat([]byte{0xFF, 0xFB, 0x90, 0x00}, 512))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/share", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	// The storage will accept any file; expect 201 Created.
	assert.Equal(t, http.StatusCreated, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
}

// TestPlayIsStateOnly verifies that POST /playback/play records the current
// track in playback state without ever driving the shared engine. Starting a
// download on a node with no active listener is what produced the "stall
// detected" log spam, the ever-incrementing StallEvents metric, and leaked
// libp2p streams; the control plane must not do it.
func TestPlayIsStateOnly(t *testing.T) {
	srv, eng := newSpyServer(t)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/playback/play", api.PlayRequest{CID: "deadbeef"})
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, decodeResponse(t, rr).Success)

	// State is recorded and visible via /playback/status.
	rr = doRequest(t, srv, http.MethodGet, "/api/v1/playback/status", nil)
	data := decodeResponse(t, rr).Data.(map[string]interface{})
	assert.Equal(t, true, data["playing"])
	assert.Equal(t, "deadbeef", data["cid"])

	// The shared engine was never asked to start a download.
	assert.Equal(t, int32(0), atomic.LoadInt32(&eng.startCalls),
		"playback/play must not call StartStreaming on a headless node")
}

// TestPlayDoesNotIncrementStallEvents is the regression guard for the reported
// bug: after calling /playback/play with no listener there must be no anti-stall
// activity, because no download loop is ever started.
func TestPlayDoesNotIncrementStallEvents(t *testing.T) {
	before := testutil.ToFloat64(metrics.StallEvents)

	srv, eng := newSpyServer(t)
	rr := doRequest(t, srv, http.MethodPost, "/api/v1/playback/play", api.PlayRequest{CID: "abc123"})
	assert.Equal(t, http.StatusOK, rr.Code)

	// No StartStreaming → no downloadLoop → no anti-stall monitor → flat metric.
	assert.Equal(t, int32(0), atomic.LoadInt32(&eng.startCalls))
	assert.Equal(t, before, testutil.ToFloat64(metrics.StallEvents),
		"playback/play must not trigger the anti-stall monitor")
}

func TestPlayMissingCID(t *testing.T) {
	srv, _ := newSpyServer(t)
	rr := doRequest(t, srv, http.MethodPost, "/api/v1/playback/play", api.PlayRequest{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.False(t, decodeResponse(t, rr).Success)
}

// TestStopAndSeekAreStateOnly verifies stop and seek mutate only in-memory state
// and likewise never reach the shared engine.
func TestStopAndSeekAreStateOnly(t *testing.T) {
	srv, eng := newSpyServer(t)

	doRequest(t, srv, http.MethodPost, "/api/v1/playback/play", api.PlayRequest{CID: "abc123"})

	rrSeek := doRequest(t, srv, http.MethodPost, "/api/v1/playback/seek", api.SeekRequest{ChunkIndex: 7})
	assert.Equal(t, http.StatusOK, rrSeek.Code)

	rrStop := doRequest(t, srv, http.MethodPost, "/api/v1/playback/stop", nil)
	assert.Equal(t, http.StatusOK, rrStop.Code)

	// Status reflects the recorded seek position and the stopped flag.
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/playback/status", nil)
	data := decodeResponse(t, rr).Data.(map[string]interface{})
	assert.Equal(t, false, data["playing"])
	assert.Equal(t, float64(7), data["chunk_index"])

	// None of the engine's download-driving methods were touched.
	assert.Equal(t, int32(0), atomic.LoadInt32(&eng.startCalls))
	assert.Equal(t, int32(0), atomic.LoadInt32(&eng.stopCalls))
	assert.Equal(t, int32(0), atomic.LoadInt32(&eng.seekCalls))
}
