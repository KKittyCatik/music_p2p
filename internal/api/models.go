package api

import "time"

// Response is the standard JSON envelope for all API responses.
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Error   string      `json:"error"`
}

// ok returns a successful Response containing data.
func ok(data interface{}) Response {
	return Response{Success: true, Data: data}
}

// fail returns an error Response.
func fail(msg string) Response {
	return Response{Success: false, Data: nil, Error: msg}
}

// StatusResponse holds node status information.
type StatusResponse struct {
	PeerID     string   `json:"peer_id"`
	Addrs      []string `json:"listen_addrs"`
	PeerCount  int      `json:"connected_peers"`
	UptimeSecs float64  `json:"uptime_secs"`
}

// TrackInfo summarises a locally stored track.
type TrackInfo struct {
	CID        string `json:"cid"`
	ChunkCount int    `json:"chunk_count"`
}

// ShareRequest is the body for POST /tracks/share.
type ShareRequest struct {
	Path     string `json:"path"`
	Announce bool   `json:"announce"`
}

// MetadataRequest is the body for POST /metadata.
type MetadataRequest struct {
	CID      string        `json:"cid"`
	Title    string        `json:"title"`
	Artist   string        `json:"artist"`
	Duration time.Duration `json:"duration"`
}

// PlayRequest is the body for POST /playback/play.
type PlayRequest struct {
	CID string `json:"cid"`
}

// SeekRequest is the body for POST /playback/seek.
type SeekRequest struct {
	ChunkIndex int `json:"chunk_index"`
}

// PlaybackStatus holds the current playback state.
type PlaybackStatus struct {
	Playing     bool   `json:"playing"`
	CID         string `json:"cid"`
	ChunkIndex  int    `json:"chunk_index"`
	BufferLevel int    `json:"buffer_level"`
}

// QueueItemRequest is used to add / insert items into the queue.
type QueueItemRequest struct {
	CID      string `json:"cid"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Position *int   `json:"position,omitempty"` // for insert
}

// ConnectRequest is the body for POST /peers/connect.
type ConnectRequest struct {
	Multiaddr string `json:"multiaddr"`
}

// PeerInfo describes a connected peer.
type PeerInfo struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// EngineStatus holds streaming engine statistics.
type EngineStatus struct {
	ActiveStreams int `json:"active_streams"`
	BandwidthBps int `json:"bandwidth_bps"`
}

// QueueState holds the full queue snapshot.
type QueueState struct {
	Current  *QueueItemResponse   `json:"current"`
	Upcoming []QueueItemResponse  `json:"upcoming"`
}

// QueueItemResponse is a serialised queue item.
type QueueItemResponse struct {
	CID    string `json:"cid"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}
