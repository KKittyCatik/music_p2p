package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/KKittyCatik/music_p2p/internal/api"
	"github.com/KKittyCatik/music_p2p/internal/audio"
	internaldht "github.com/KKittyCatik/music_p2p/internal/dht"
	"github.com/KKittyCatik/music_p2p/internal/logging"
	"github.com/KKittyCatik/music_p2p/internal/metadata"
	"github.com/KKittyCatik/music_p2p/internal/metrics"
	"github.com/KKittyCatik/music_p2p/internal/p2p"
	"github.com/KKittyCatik/music_p2p/internal/queue"
	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/KKittyCatik/music_p2p/internal/storage"
	"github.com/KKittyCatik/music_p2p/internal/streaming"
)

func main() {
	var (
		listenPort  = flag.Int("listen", 4001, "TCP port to listen on")
		connectAddr = flag.String("connect", "", "peer multiaddr to connect to")
		playCID     = flag.String("play", "", "CID of the track to play")
		searchQ     = flag.String("search", "", "search query for tracks")
		sharePath   = flag.String("share", "", "path to a local MP3 file to share")
		announce    = flag.Bool("announce", false, "announce shared tracks to the DHT")
		queueCIDs   = flag.String("queue", "", "comma-separated list of CIDs to enqueue for autoplay")
		apiPort     = flag.Int("api-port", 0, "port for the REST API server (0 = disabled)")
		metricsPort = flag.Int("metrics-port", 0, "port for the Prometheus metrics server (0 = disabled)")
		logLevel    = flag.String("log-level", "info", "log level: debug, info, warn, error")
	)
	flag.Parse()

	logging.Init(*logLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Start libp2p node
	h, err := p2p.StartNode(*listenPort)
	if err != nil {
		log.Fatalf("start node: %v", err)
	}
	defer h.Close()
	log.Printf("Node ID: %s", h.ID())
	for _, addr := range h.Addrs() {
		log.Printf("Listening on: %s/p2p/%s", addr, h.ID())
	}

	// 2. Create storage and scorer
	stor := storage.New(".")
	sc := scoring.NewScorer()

	// 3. Create DHT
	dhtNode, err := internaldht.NewDHT(ctx, h)
	if err != nil {
		log.Fatalf("create dht: %v", err)
	}
	if err := dhtNode.Bootstrap(ctx); err != nil {
		log.Printf("dht bootstrap warning: %v", err)
	}

	// 4. Create metadata store
	metaStore, err := metadata.NewStore(ctx, h)
	if err != nil {
		log.Fatalf("create metadata store: %v", err)
	}

	// 5. Create streaming engine and register stream handler
	engine := streaming.NewEngine(h, dhtNode, stor, sc)
	h.SetStreamHandler("/music/1.0.0", engine.ServeStream)

	// 6. Register chunk handler for direct chunk serving
	p2p.SetChunkHandler(h, func(cid string, index int) ([]byte, error) {
		chunk, err := stor.GetChunk(cid, index)
		if err != nil {
			return nil, err
		}
		return streaming.ChunkBytes(chunk), nil
	})

	// Shared playback queue (used by both CLI and API).
	sharedQueue := queue.New()

	// 7. Start REST API server if --api-port is set.
	if *apiPort > 0 {
		apiSrv := api.New(api.Config{
			Storage:  stor,
			Metadata: metaStore,
			Queue:    sharedQueue,
			Scorer:   sc,
			Engine:   engine,
			DHT:      dhtNode,
			Host:     h,
		})
		apiAddr := fmt.Sprintf(":%d", *apiPort)
		log.Printf("API server listening on %s", apiAddr)
		go func() {
			if err := apiSrv.Run(ctx, apiAddr); err != nil && err != http.ErrServerClosed {
				log.Printf("api server: %v", err)
			}
		}()
	}

	// Start Prometheus metrics server if --metrics-port is set.
	if *metricsPort > 0 {
		metricsAddr := fmt.Sprintf(":%d", *metricsPort)
		log.Printf("Metrics server listening on %s", metricsAddr)
		go func() {
			if err := metrics.StartMetricsServer(metricsAddr); err != nil {
				log.Printf("metrics server: %v", err)
			}
		}()
	}

	// Connect to a peer if requested
	if *connectAddr != "" {
		if err := p2p.Connect(h, *connectAddr); err != nil {
			log.Printf("connect to %s: %v", *connectAddr, err)
		} else {
			log.Printf("Connected to %s", *connectAddr)
			metrics.PeersTotal.Inc()
			metrics.PeersConnected.Set(float64(len(h.Network().Peers())))
		}
	}

	// --share: load and optionally announce a local MP3
	if *sharePath != "" {
		cid, err := stor.LoadTrack(*sharePath)
		if err != nil {
			log.Fatalf("load track %s: %v", *sharePath, err)
		}
		log.Printf("Loaded track %s with CID %s (%d chunks)", *sharePath, cid, stor.GetTotalChunks(cid))

		meta := metadata.TrackMetadata{
			CID:    cid,
			Title:  *sharePath,
			Artist: "Unknown",
		}
		metaStore.AddLocal(meta)

		if *announce {
			if err := dhtNode.Provide(ctx, cid); err != nil {
				log.Printf("provide %s: %v", cid, err)
			} else {
				log.Printf("Announced %s to DHT", cid)
			}
			if err := metaStore.Publish(meta); err != nil {
				log.Printf("publish metadata: %v", err)
			}
		}
	}

	// --search: search local metadata store and exit
	if *searchQ != "" {
		results := metaStore.Search(*searchQ)
		if len(results) == 0 {
			fmt.Println("No results found.")
		}
		for _, m := range results {
			fmt.Printf("CID: %s\n  MetaID: %s\n  Title:  %s\n  Artist: %s\n  Duration: %s\n",
				m.CID, m.MetaID, m.Title, m.Artist, m.Duration)
		}
		return
	}

	// --play / --queue: stream and play tracks
	if *playCID != "" || *queueCIDs != "" {
		q := sharedQueue

		// Enqueue the primary CID first.
		if *playCID != "" {
			q.Enqueue(queue.Item{CID: *playCID})
		}

		// Enqueue additional CIDs from --queue flag.
		if *queueCIDs != "" {
			for _, cid := range splitCSV(*queueCIDs) {
				if cid != "" {
					q.Enqueue(queue.Item{CID: cid})
				}
			}
		}

		player := audio.NewPlayer()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		for {
			item, ok := q.Next()
			if !ok {
				log.Println("Queue empty. Playback finished.")
				break
			}

			log.Printf("Starting stream for CID %s …", item.CID)
			if err := engine.StartStreaming(ctx, item.CID); err != nil {
				log.Printf("start streaming %s: %v", item.CID, err)
				continue
			}
			log.Printf("Streaming %s …", item.CID)

			// Instant playback: wait for the minimum buffer before starting audio.
			if err := waitForBuffer(ctx, engine, audio.InstantPlaybackChunks); err != nil {
				log.Printf("buffer wait cancelled for %s", item.CID)
				break
			}

			if err := player.Play(engine); err != nil {
				log.Printf("play %s: %v", item.CID, err)
				engine.Stop()
				continue
			}

			// Gapless: preload next track when this one is nearing its end.
			go preloadNext(ctx, q, engine, player)

			log.Printf("Playing %s … press Ctrl-C to stop", item.CID)
			for player.IsPlaying() {
				select {
				case <-sigCh:
					player.Stop()
					engine.Stop()
					return
				case <-time.After(200 * time.Millisecond):
				}
			}
			engine.Stop()
		}
		return
	}

	// Default: keep node running to serve peers
	log.Println("Node running. Press Ctrl-C to exit.")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down.")
}

// waitForBuffer blocks until at least minChunks have been received by engine,
// or ctx is cancelled.  This implements instant-playback: audio starts after
// a minimal initial buffer (≤ 0.5 s worth of data).
func waitForBuffer(ctx context.Context, eng *streaming.Engine, minChunks int) error {
	return eng.WaitForChunks(ctx, minChunks)
}

// preloadNext monitors the current track's remaining time and registers the
// next track reader with the player for gapless transition.
func preloadNext(ctx context.Context, q *queue.Queue, engine *streaming.Engine, player *audio.Player) {
	doneCh := player.CurrentTrackDone()
	if doneCh == nil {
		return
	}
	// Wait until the current track signals "done" (fired when stream exhausted).
	// For gapless we want to act 2 s before end; since we don't have exact
	// duration info here, we arm on doneCh itself which fires at the boundary.
	select {
	case <-doneCh:
	case <-ctx.Done():
		return
	}
	// Try to preload the next item.
	item, ok := q.Peek()
	if !ok {
		return
	}
	log.Printf("gapless: preloading next track %s", item.CID)
	nextEngine := streaming.NewEngine(engine.Host(), engine.DHT(), engine.Storage(), engine.Scorer())
	if err := nextEngine.StartStreaming(ctx, item.CID); err != nil {
		log.Printf("gapless: failed to start next stream: %v", err)
		return
	}
	player.SetNext(nextEngine)
}

// splitCSV splits a comma-separated string into trimmed tokens.
func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			tok := strings.TrimSpace(s[start:i])
			out = append(out, tok)
			start = i + 1
		}
	}
	return out
}
