package p2p

import (
	"context"
	"fmt"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

const MusicProtocol = "/music/1.0.0"

// StartNode creates a libp2p host listening on the given TCP port.
//
// NAT traversal is enabled so nodes behind home routers can be reached without
// any project-operated server (see specs/001-invite-connect):
//   - NATPortMap: automatic UPnP / NAT-PMP port mapping on cooperating routers —
//     this is what actually makes a home-NAT'd node directly reachable.
//   - EnableNATService + identify: AutoNAT, so the node learns its observed
//     external address (advertised in invite codes).
//
// DCUtR hole punching is intentionally NOT enabled: it only works through a
// relay (the relayed connection is the signaling channel for the simultaneous
// open), and we operate no relay. Enabling it without a relay is inert. Closing
// the remaining "hard NAT" cases needs a self-organizing peer-relay mesh
// (publicly-reachable nodes acting as relays + AutoRelay) — tracked separately.
func StartNode(port int) (host.Host, error) {
	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(listenAddr),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
	)
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}
	return h, nil
}

// Connect dials the given peer multiaddr and adds it to the peerstore.
func Connect(h host.Host, peerAddr string) error {
	ma, err := multiaddr.NewMultiaddr(peerAddr)
	if err != nil {
		return fmt.Errorf("parse multiaddr %q: %w", peerAddr, err)
	}
	info, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		return fmt.Errorf("addr info from multiaddr: %w", err)
	}
	if err := h.Connect(context.Background(), *info); err != nil {
		return fmt.Errorf("connect to peer %s: %w", info.ID, err)
	}
	return nil
}
