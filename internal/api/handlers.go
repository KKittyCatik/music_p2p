package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/KKittyCatik/music_p2p/internal/metadata"
	"github.com/KKittyCatik/music_p2p/internal/metrics"
	"github.com/KKittyCatik/music_p2p/internal/queue"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// handleGetStatus returns the current node status.
//
// @Summary      Node status
// @Description  Returns the node's peer ID, listen addresses, connected peer count, and uptime.
// @Tags         node
// @Produce      json
// @Success      200  {object}  Response{data=StatusResponse}
// @Failure      500  {object}  Response
// @Router       /status [get]
func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	var resp StatusResponse
	if s.hostInfo != nil {
		resp.PeerID = s.hostInfo.ID().String()
		for _, a := range s.hostInfo.Addrs() {
			resp.Addrs = append(resp.Addrs, a.String())
		}
		resp.PeerCount = len(s.hostInfo.Network().Peers())
	}
	resp.UptimeSecs = s.uptime()
	writeJSON(w, ok(resp))
}

// uptime returns the server uptime in seconds.
func (s *Server) uptime() float64 {
	return time.Since(s.startTime).Seconds()
}

// handleGetTracks lists all locally stored tracks.
//
// @Summary      List tracks
// @Description  Returns the CID and chunk count for every locally stored track.
// @Tags         tracks
// @Produce      json
// @Success      200  {object}  Response{data=[]TrackInfo}
// @Router       /tracks [get]
func (s *Server) handleGetTracks(w http.ResponseWriter, r *http.Request) {
	if s.stor == nil {
		writeJSON(w, ok([]TrackInfo{}))
		return
	}
	cids := s.stor.ListTracks()
	tracks := make([]TrackInfo, 0, len(cids))
	for _, cid := range cids {
		tracks = append(tracks, TrackInfo{
			CID:        cid,
			ChunkCount: s.stor.GetTotalChunks(cid),
		})
	}
	writeJSON(w, ok(tracks))
}

// handleShareTrack uploads an MP3 file, saves it temporarily, and optionally announces it.
//
// @Summary      Share a track
// @Description  Upload an MP3 file into storage and optionally announce it to the DHT.
// @Tags         tracks
// @Accept       multipart/form-data
// @Produce      json
// @Param        file formData file true "MP3 File to upload"
// @Param        announce formData bool false "Announce to DHT"
// @Success      201   {object}  Response{data=TrackInfo}
// @Failure      400   {object}  Response
// @Failure      500   {object}  Response
// @Router       /tracks/share [post]
func (s *Server) handleShareTrack(w http.ResponseWriter, r *http.Request) {
	if s.stor == nil {
		writeStatus(w, http.StatusServiceUnavailable, fail("storage not available"))
		return
	}

	// Парсим multipart-форму (ограничение памяти 10 МБ)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeStatus(w, http.StatusBadRequest, fail("failed to parse form: "+err.Error()))
		return
	}

	// Получаем файл из формы
	file, _, err := r.FormFile("file")
	if err != nil {
		writeStatus(w, http.StatusBadRequest, fail("file is required: "+err.Error()))
		return
	}
	defer file.Close()

	// Получаем флаг announce
	announce := r.FormValue("announce") == "true"

	// Создаем временный файл в контейнере для сохранения загруженного MP3
	tempFile, err := os.CreateTemp("", "upload-*.mp3")
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, fail("failed to create temp file: "+err.Error()))
		return
	}
	// Обязательно удаляем временный файл после загрузки в хранилище
	defer os.Remove(tempFile.Name())

	// Копируем содержимое загруженного файла во временный файл
	if _, err := io.Copy(tempFile, file); err != nil {
		tempFile.Close()
		writeStatus(w, http.StatusInternalServerError, fail("failed to save file: "+err.Error()))
		return
	}
	tempFile.Close()

	// Вызываем уже существующий метод LoadTrack, передав путь к временному файлу
	cid, err := s.stor.LoadTrack(tempFile.Name())
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, fail("load track: "+err.Error()))
		return
	}

	if announce && s.dht != nil {
		if err := s.dht.Provide(r.Context(), cid); err != nil {
			// Ошибка не фатальна – просто игнорируем/логируем
			_ = err
		}
	}

	info := TrackInfo{CID: cid, ChunkCount: s.stor.GetTotalChunks(cid)}
	writeStatus(w, http.StatusCreated, ok(info))
}

// handleDeleteTrack removes a track from local storage.
//
// @Summary      Delete a track
// @Description  Remove a track and all its chunks from local storage.
// @Tags         tracks
// @Produce      json
// @Param        cid  path      string  true  "Track CID"
// @Success      200  {object}  Response
// @Failure      404  {object}  Response
// @Router       /tracks/{cid} [delete]
func (s *Server) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
	cid := mux.Vars(r)["cid"]
	if s.stor == nil {
		writeStatus(w, http.StatusServiceUnavailable, fail("storage not available"))
		return
	}
	if err := s.stor.RemoveTrack(cid); err != nil {
		writeStatus(w, http.StatusNotFound, fail(err.Error()))
		return
	}
	writeJSON(w, ok(nil))
}

// handleGetAllMetadata returns all metadata entries.
//
// @Summary      List metadata
// @Description  Returns all locally known track metadata entries.
// @Tags         metadata
// @Produce      json
// @Success      200  {object}  Response{data=[]metadata.TrackMetadata}
// @Router       /metadata [get]
func (s *Server) handleGetAllMetadata(w http.ResponseWriter, r *http.Request) {
	if s.meta == nil {
		writeJSON(w, ok([]metadata.TrackMetadata{}))
		return
	}
	writeJSON(w, ok(s.meta.All()))
}

// handleSearchMetadata searches metadata by title or artist.
//
// @Summary      Search metadata
// @Description  Case-insensitive search over track title and artist fields.
// @Tags         metadata
// @Produce      json
// @Param        q    query     string  true  "Search query"
// @Success      200  {object}  Response{data=[]metadata.TrackMetadata}
// @Router       /metadata/search [get]
func (s *Server) handleSearchMetadata(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if s.meta == nil {
		writeJSON(w, ok([]metadata.TrackMetadata{}))
		return
	}
	writeJSON(w, ok(s.meta.Search(q)))
}

// handleGetMetadata returns metadata for a specific CID.
//
// @Summary      Get metadata by CID
// @Description  Returns the TrackMetadata for the given CID.
// @Tags         metadata
// @Produce      json
// @Param        cid  path      string  true  "Track CID"
// @Success      200  {object}  Response{data=metadata.TrackMetadata}
// @Failure      404  {object}  Response
// @Router       /metadata/{cid} [get]
func (s *Server) handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	cid := mux.Vars(r)["cid"]
	if s.meta == nil {
		writeStatus(w, http.StatusNotFound, fail("not found"))
		return
	}
	m, ok2 := s.meta.Get(cid)
	if !ok2 {
		writeStatus(w, http.StatusNotFound, fail("metadata not found for CID "+cid))
		return
	}
	writeJSON(w, ok(m))
}

// handlePublishMetadata publishes new metadata.
//
// @Summary      Publish metadata
// @Description  Store and broadcast a new TrackMetadata entry via gossipsub.
// @Tags         metadata
// @Accept       json
// @Produce      json
// @Param        body  body      MetadataRequest  true  "Metadata"
// @Success      201   {object}  Response{data=metadata.TrackMetadata}
// @Failure      400   {object}  Response
// @Failure      500   {object}  Response
// @Router       /metadata [post]
func (s *Server) handlePublishMetadata(w http.ResponseWriter, r *http.Request) {
	if s.meta == nil {
		writeStatus(w, http.StatusServiceUnavailable, fail("metadata store not available"))
		return
	}
	var req MetadataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStatus(w, http.StatusBadRequest, fail("invalid request: "+err.Error()))
		return
	}
	meta := metadata.TrackMetadata{
		CID:      req.CID,
		Title:    req.Title,
		Artist:   req.Artist,
		Duration: req.Duration,
	}
	s.meta.AddLocal(meta)
	if err := s.meta.Publish(meta); err != nil {
		// Non-fatal if gossipsub is unavailable.
		_ = err
	}
	writeStatus(w, http.StatusCreated, ok(meta))
}

// handlePlay starts streaming a track.
//
// @Summary      Start playback
// @Description  Begin streaming the track identified by the given CID.
// @Tags         playback
// @Accept       json
// @Produce      json
// @Param        body  body      PlayRequest  true  "Play request"
// @Success      200   {object}  Response{data=PlaybackStatus}
// @Failure      400   {object}  Response
// @Failure      500   {object}  Response
// @Router       /playback/play [post]
func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	var req PlayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStatus(w, http.StatusBadRequest, fail("invalid request: "+err.Error()))
		return
	}
	if req.CID == "" {
		writeStatus(w, http.StatusBadRequest, fail("cid is required"))
		return
	}

	if s.engine != nil {
		if err := s.engine.StartStreaming(context.Background(), req.CID); err != nil {
			writeStatus(w, http.StatusInternalServerError, fail("start streaming: "+err.Error()))
			return
		}
	}

	s.playMu.Lock()
	s.playingCID = req.CID
	s.isPlaying = true
	s.chunkIndex = 0
	s.playMu.Unlock()

	metrics.TracksPlayed.Inc()
	metrics.PlaybackState.Set(1)
	writeJSON(w, ok(s.playbackStatus()))
}

// handleStop stops the current playback.
//
// @Summary      Stop playback
// @Description  Stop the currently playing track.
// @Tags         playback
// @Produce      json
// @Success      200  {object}  Response{data=PlaybackStatus}
// @Router       /playback/stop [post]
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if s.engine != nil {
		s.engine.Stop()
	}
	s.playMu.Lock()
	s.isPlaying = false
	s.playMu.Unlock()
	metrics.PlaybackState.Set(0)
	writeJSON(w, ok(s.playbackStatus()))
}

// handleSeek seeks to a specific chunk position.
//
// @Summary      Seek playback
// @Description  Jump to the given chunk index within the current track.
// @Tags         playback
// @Accept       json
// @Produce      json
// @Param        body  body      SeekRequest  true  "Seek request"
// @Success      200   {object}  Response{data=PlaybackStatus}
// @Failure      400   {object}  Response
// @Router       /playback/seek [post]
func (s *Server) handleSeek(w http.ResponseWriter, r *http.Request) {
	var req SeekRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStatus(w, http.StatusBadRequest, fail("invalid request: "+err.Error()))
		return
	}
	if s.engine != nil {
		s.engine.Seek(req.ChunkIndex)
	}
	s.playMu.Lock()
	s.chunkIndex = req.ChunkIndex
	s.playMu.Unlock()
	writeJSON(w, ok(s.playbackStatus()))
}

// handlePlaybackStatus returns the current playback state.
//
// @Summary      Playback status
// @Description  Returns whether a track is playing, the current CID, position, and buffer level.
// @Tags         playback
// @Produce      json
// @Success      200  {object}  Response{data=PlaybackStatus}
// @Router       /playback/status [get]
func (s *Server) handlePlaybackStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, ok(s.playbackStatus()))
}

// playbackStatus returns a snapshot of the current playback state (lock-safe).
func (s *Server) playbackStatus() PlaybackStatus {
	s.playMu.Lock()
	defer s.playMu.Unlock()
	return PlaybackStatus{
		Playing:    s.isPlaying,
		CID:        s.playingCID,
		ChunkIndex: s.chunkIndex,
	}
}

// handleGetQueue returns the current queue state.
//
// @Summary      Get queue
// @Description  Returns the current item, upcoming items, and play history.
// @Tags         queue
// @Produce      json
// @Success      200  {object}  Response{data=QueueState}
// @Router       /queue [get]
func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	state := QueueState{Upcoming: []QueueItemResponse{}}
	if s.q != nil {
		if cur, ok2 := s.q.Current(); ok2 {
			qi := toQueueItemResponse(cur)
			state.Current = &qi
		}
		for _, item := range s.q.Upcoming() {
			state.Upcoming = append(state.Upcoming, toQueueItemResponse(item))
		}
	}
	writeJSON(w, ok(state))
}

// handleEnqueue adds an item to the queue.
//
// @Summary      Enqueue item
// @Description  Add a track to the end of the playback queue.
// @Tags         queue
// @Accept       json
// @Produce      json
// @Param        body  body      QueueItemRequest  true  "Queue item"
// @Success      201   {object}  Response{data=QueueState}
// @Failure      400   {object}  Response
// @Router       /queue [post]
func (s *Server) handleEnqueue(w http.ResponseWriter, r *http.Request) {
	var req QueueItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStatus(w, http.StatusBadRequest, fail("invalid request: "+err.Error()))
		return
	}
	if req.CID == "" {
		writeStatus(w, http.StatusBadRequest, fail("cid is required"))
		return
	}
	if s.q != nil {
		s.q.Enqueue(queue.Item{CID: req.CID, Title: req.Title, Artist: req.Artist})
	}
	state := QueueState{Upcoming: []QueueItemResponse{}}
	if s.q != nil {
		for _, item := range s.q.Upcoming() {
			state.Upcoming = append(state.Upcoming, toQueueItemResponse(item))
		}
	}
	writeStatus(w, http.StatusCreated, ok(state))
}

// handleInsertQueue inserts an item at a specific position.
//
// @Summary      Insert queue item
// @Description  Insert a track at the given position in the queue (0 = next to play).
// @Tags         queue
// @Accept       json
// @Produce      json
// @Param        body  body      QueueItemRequest  true  "Queue item with position"
// @Success      201   {object}  Response{data=QueueState}
// @Failure      400   {object}  Response
// @Router       /queue/insert [post]
func (s *Server) handleInsertQueue(w http.ResponseWriter, r *http.Request) {
	var req QueueItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStatus(w, http.StatusBadRequest, fail("invalid request: "+err.Error()))
		return
	}
	if req.CID == "" {
		writeStatus(w, http.StatusBadRequest, fail("cid is required"))
		return
	}
	pos := 0
	if req.Position != nil {
		pos = *req.Position
	}
	if s.q != nil {
		s.q.Insert(pos, queue.Item{CID: req.CID, Title: req.Title, Artist: req.Artist})
	}
	state := QueueState{Upcoming: []QueueItemResponse{}}
	if s.q != nil {
		for _, item := range s.q.Upcoming() {
			state.Upcoming = append(state.Upcoming, toQueueItemResponse(item))
		}
	}
	writeStatus(w, http.StatusCreated, ok(state))
}

// handleClearQueue clears the upcoming queue.
//
// @Summary      Clear queue
// @Description  Remove all upcoming items from the queue (history is preserved).
// @Tags         queue
// @Produce      json
// @Success      200  {object}  Response
// @Router       /queue [delete]
func (s *Server) handleClearQueue(w http.ResponseWriter, r *http.Request) {
	if s.q != nil {
		s.q.Clear()
	}
	writeJSON(w, ok(nil))
}

// handleGetHistory returns the playback history.
//
// @Summary      Queue history
// @Description  Returns all previously played items, oldest first.
// @Tags         queue
// @Produce      json
// @Success      200  {object}  Response{data=[]QueueItemResponse}
// @Router       /queue/history [get]
func (s *Server) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	items := []QueueItemResponse{}
	if s.q != nil {
		for _, item := range s.q.History() {
			items = append(items, toQueueItemResponse(item))
		}
	}
	writeJSON(w, ok(items))
}

// handleGetPeers returns connected peers with scores.
//
// @Summary      List peers
// @Description  Returns all currently connected peers along with their composite score.
// @Tags         peers
// @Produce      json
// @Success      200  {object}  Response{data=[]PeerInfo}
// @Router       /peers [get]
func (s *Server) handleGetPeers(w http.ResponseWriter, r *http.Request) {
	peers := []PeerInfo{}
	if s.hostInfo != nil {
		for _, id := range s.hostInfo.Network().Peers() {
			score := 0.5
			if s.scorer != nil {
				score = s.scorer.Score(id)
			}
			peers = append(peers, PeerInfo{ID: id.String(), Score: score})
		}
	}
	writeJSON(w, ok(peers))
}

// handleConnectPeer connects to a peer by multiaddr.
//
// @Summary      Connect to peer
// @Description  Open a connection to the specified peer multiaddr.
// @Tags         peers
// @Accept       json
// @Produce      json
// @Param        body  body      ConnectRequest  true  "Peer multiaddr"
// @Success      200   {object}  Response
// @Failure      400   {object}  Response
// @Failure      500   {object}  Response
// @Router       /peers/connect [post]
func (s *Server) handleConnectPeer(w http.ResponseWriter, r *http.Request) {
	var req ConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStatus(w, http.StatusBadRequest, fail("invalid request: "+err.Error()))
		return
	}
	if req.Multiaddr == "" {
		writeStatus(w, http.StatusBadRequest, fail("multiaddr is required"))
		return
	}
	if s.hostInfo == nil {
		writeStatus(w, http.StatusServiceUnavailable, fail("host not available"))
		return
	}
	maddr, err := ma.NewMultiaddr(req.Multiaddr)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, fail("invalid multiaddr: "+err.Error()))
		return
	}
	pi, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, fail("parse peer addr: "+err.Error()))
		return
	}
	if err := s.hostInfo.Connect(r.Context(), *pi); err != nil {
		writeStatus(w, http.StatusInternalServerError, fail("connect: "+err.Error()))
		return
	}
	writeJSON(w, ok(nil))
}

// handleGetPeerScore returns the score for a specific peer.
//
// @Summary      Peer score
// @Description  Returns the composite score for the given peer ID.
// @Tags         peers
// @Produce      json
// @Param        peerID  path      string  true  "Peer ID"
// @Success      200     {object}  Response{data=PeerInfo}
// @Failure      400     {object}  Response
// @Router       /peers/{peerID}/score [get]
func (s *Server) handleGetPeerScore(w http.ResponseWriter, r *http.Request) {
	peerIDStr := mux.Vars(r)["peerID"]
	id, err := peer.Decode(peerIDStr)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, fail("invalid peer ID: "+err.Error()))
		return
	}
	score := 0.5
	if s.scorer != nil {
		score = s.scorer.Score(id)
	}
	writeJSON(w, ok(PeerInfo{ID: peerIDStr, Score: score}))
}

// handleDHTProvide announces a track to the DHT.
//
// @Summary      DHT provide
// @Description  Announce the given CID to the DHT so other peers can find it.
// @Tags         dht
// @Produce      json
// @Param        cid  path      string  true  "Track CID"
// @Success      200  {object}  Response
// @Failure      500  {object}  Response
// @Router       /dht/provide/{cid} [post]
func (s *Server) handleDHTProvide(w http.ResponseWriter, r *http.Request) {
	cid := mux.Vars(r)["cid"]
	if s.dht == nil {
		writeStatus(w, http.StatusServiceUnavailable, fail("DHT not available"))
		return
	}
	if err := s.dht.Provide(r.Context(), cid); err != nil {
		writeStatus(w, http.StatusInternalServerError, fail("provide: "+err.Error()))
		return
	}
	writeJSON(w, ok(nil))
}

// handleDHTProviders finds providers for a CID in the DHT.
//
// @Summary      DHT providers
// @Description  Find peers that have announced the given CID to the DHT.
// @Tags         dht
// @Produce      json
// @Param        cid  path      string  true  "Track CID"
// @Success      200  {object}  Response{data=[]PeerInfo}
// @Failure      500  {object}  Response
// @Router       /dht/providers/{cid} [get]
func (s *Server) handleDHTProviders(w http.ResponseWriter, r *http.Request) {
	cid := mux.Vars(r)["cid"]
	if s.dht == nil {
		writeJSON(w, ok([]PeerInfo{}))
		return
	}
	addrs, err := s.dht.FindProviders(r.Context(), cid)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, fail("find providers: "+err.Error()))
		return
	}
	infos := make([]PeerInfo, 0, len(addrs))
	for _, a := range addrs {
		infos = append(infos, PeerInfo{ID: a.ID.String()})
	}
	writeJSON(w, ok(infos))
}

// handleEngineStatus returns streaming engine statistics.
//
// @Summary      Engine status
// @Description  Returns the current bandwidth estimate and number of active streams.
// @Tags         engine
// @Produce      json
// @Success      200  {object}  Response{data=EngineStatus}
// @Router       /engine/status [get]
func (s *Server) handleEngineStatus(w http.ResponseWriter, r *http.Request) {
	status := EngineStatus{}
	if s.engine != nil {
		status.BandwidthBps = s.engine.AdaptiveBitrate().CurrentBandwidth()
	}
	writeJSON(w, ok(status))
}

// toQueueItemResponse converts a queue.Item to a QueueItemResponse.
func toQueueItemResponse(item queue.Item) QueueItemResponse {
	return QueueItemResponse{CID: item.CID, Title: item.Title, Artist: item.Artist}
}
