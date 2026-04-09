package discovery

import (
	"context"
	"log"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

const mdnsServiceTag = "music-p2p-discovery"

type mdnsNotifee struct {
	h   host.Host
	ctx context.Context
}

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.h.ID() {
		return
	}
	log.Printf("mdns: discovered peer %s, connecting...", pi.ID.ShortString())
	if err := n.h.Connect(n.ctx, pi); err != nil {
		log.Printf("mdns: failed to connect to %s: %v", pi.ID.ShortString(), err)
	} else {
		log.Printf("mdns: connected to %s", pi.ID.ShortString())
	}
}

// StartMDNS starts an mDNS discovery service on the given host.
// Discovered peers on the local network are automatically connected to.
func StartMDNS(ctx context.Context, h host.Host) error {
	s := mdns.NewMdnsService(h, mdnsServiceTag, &mdnsNotifee{h: h, ctx: ctx})
	return s.Start()
}
