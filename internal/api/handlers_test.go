package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/KKittyCatik/music_p2p/internal/api"
	"github.com/KKittyCatik/music_p2p/internal/metadata"
	"github.com/KKittyCatik/music_p2p/internal/queue"
	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/KKittyCatik/music_p2p/internal/storage"
)

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
