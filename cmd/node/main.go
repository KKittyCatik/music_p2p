package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	internaldht "github.com/KKittyCatik/music_p2p/internal/dht"
	"github.com/KKittyCatik/music_p2p/internal/audio"
	"github.com/KKittyCatik/music_p2p/internal/metadata"
	"github.com/KKittyCatik/music_p2p/internal/p2p"
	"github.com/KKittyCatik/music_p2p/internal/scoring"
	"github.com/KKittyCatik/music_p2p/internal/storage"
	"github.com/KKittyCatik/music_p2p/internal/streaming"
)

func main() {
	var (
		listenPort = flag.Int("listen", 4001, "TCP port to listen on")
		connectAddr = flag.String("connect", "", "peer multiaddr to connect to")
		playCID    = flag.String("play", "", "CID of the track to play")
		searchQ    = flag.String("search", "", "search query for tracks")
		sharePath  = flag.String("share", "", "path to a local MP3 file to share")
		announce   = flag.Bool("announce", false, "announce shared tracks to the DHT")
	)
	flag.Parse()

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

	// Connect to a peer if requested
	if *connectAddr != "" {
		if err := p2p.Connect(h, *connectAddr); err != nil {
			log.Printf("connect to %s: %v", *connectAddr, err)
		} else {
			log.Printf("Connected to %s", *connectAddr)
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
			fmt.Printf("CID: %s\n  Title:  %s\n  Artist: %s\n  Duration: %s\n",
				m.CID, m.Title, m.Artist, m.Duration)
		}
		return
	}

	// --play: stream and play a track by CID
	if *playCID != "" {
		if err := engine.StartStreaming(ctx, *playCID); err != nil {
			log.Fatalf("start streaming %s: %v", *playCID, err)
		}
		log.Printf("Streaming %s …", *playCID)

		player := audio.NewPlayer()
		if err := player.Play(engine); err != nil {
			log.Fatalf("play: %v", err)
		}

		log.Println("Playing … press Ctrl-C to stop")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		for player.IsPlaying() {
			select {
			case <-sigCh:
				player.Stop()
				engine.Stop()
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
		log.Println("Playback finished.")
		return
	}

	// Default: keep node running to serve peers
	log.Println("Node running. Press Ctrl-C to exit.")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down.")
}
