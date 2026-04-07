package p2p

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

const MusicProtocol = "/music/1.0.0"

// ChunkRequestHandler is called when a remote peer requests a chunk.
type ChunkRequestHandler func(cid string, index int) ([]byte, error)

// StartNode creates a libp2p host listening on the given TCP port.
func StartNode(port int) (host.Host, error) {
	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)
	h, err := libp2p.New(libp2p.ListenAddrStrings(listenAddr))
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}
	return h, nil
}

// SetChunkHandler registers a ChunkRequestHandler on the host for the music protocol.
func SetChunkHandler(h host.Host, handler ChunkRequestHandler) {
	h.SetStreamHandler(MusicProtocol, func(s network.Stream) {
		handleStream(s, handler)
	})
}

// handleStream reads "GET <cid> <index>\n" and writes chunk bytes or "ERR\n".
func handleStream(s network.Stream, handler ChunkRequestHandler) {
	defer s.Close()

	scanner := bufio.NewScanner(s)
	if !scanner.Scan() {
		return
	}
	line := strings.TrimSpace(scanner.Text())
	parts := strings.Fields(line)
	if len(parts) != 3 || parts[0] != "GET" {
		fmt.Fprintf(s, "ERR\n")
		return
	}
	cid := parts[1]
	index, err := strconv.Atoi(parts[2])
	if err != nil {
		fmt.Fprintf(s, "ERR\n")
		return
	}

	data, err := handler(cid, index)
	if err != nil {
		fmt.Fprintf(s, "ERR\n")
		return
	}
	s.Write(data)
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
