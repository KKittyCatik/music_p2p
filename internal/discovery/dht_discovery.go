package discovery

import (
	"context"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	kaddht "github.com/libp2p/go-libp2p-kad-dht"
)

const (
	// RendezvousNamespace is the shared namespace all music_p2p nodes advertise under.
	RendezvousNamespace = "music-p2p-network"
	discoveryInterval   = 30 * time.Second
)

// StartDHTDiscovery advertises this node under RendezvousNamespace and
// periodically discovers and connects to peers via DHT rendezvous.
func StartDHTDiscovery(ctx context.Context, h host.Host, dht *kaddht.IpfsDHT) {
	routingDiscovery := drouting.NewRoutingDiscovery(dht)

	dutil.Advertise(ctx, routingDiscovery, RendezvousNamespace)
	log.Printf("dht-discovery: advertising under namespace %q", RendezvousNamespace)

	go func() {
		ticker := time.NewTicker(discoveryInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				peerCh, err := routingDiscovery.FindPeers(ctx, RendezvousNamespace)
				if err != nil {
					log.Printf("dht-discovery: FindPeers error: %v", err)
					continue
				}
				for pi := range peerCh {
					if pi.ID == h.ID() || pi.ID == "" {
						continue
					}
					if h.Network().Connectedness(pi.ID) == network.Connected {
						continue
					}
					log.Printf("dht-discovery: found peer %s, connecting...", pi.ID.ShortString())
					if err := h.Connect(ctx, pi); err != nil {
						log.Printf("dht-discovery: failed to connect to %s: %v", pi.ID.ShortString(), err)
					}
				}
			}
		}
	}()
}
