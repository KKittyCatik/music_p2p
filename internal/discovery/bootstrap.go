package discovery

import (
	"context"
	"log"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// DefaultBootstrapPeers can be populated with well-known nodes.
// Empty by default — users provide their own via --bootstrap flag.
var DefaultBootstrapPeers []string

// Bootstrap connects to each peer in addrs in parallel and returns the number
// of successful connections.
func Bootstrap(ctx context.Context, h host.Host, addrs []string) int {
	var wg sync.WaitGroup
	connected := 0
	var mu sync.Mutex

	for _, addr := range addrs {
		if addr == "" {
			continue
		}
		wg.Add(1)
		go func(a string) {
			defer wg.Done()
			maddr, err := ma.NewMultiaddr(a)
			if err != nil {
				log.Printf("bootstrap: invalid addr %q: %v", a, err)
				return
			}
			pi, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				log.Printf("bootstrap: parse %q: %v", a, err)
				return
			}
			if pi.ID == h.ID() {
				return
			}
			log.Printf("bootstrap: connecting to %s ...", pi.ID.ShortString())
			if err := h.Connect(ctx, *pi); err != nil {
				log.Printf("bootstrap: failed to connect to %s: %v", pi.ID.ShortString(), err)
				return
			}
			mu.Lock()
			connected++
			mu.Unlock()
			log.Printf("bootstrap: connected to %s", pi.ID.ShortString())
		}(addr)
	}
	wg.Wait()
	return connected
}
