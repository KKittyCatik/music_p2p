package dht

import (
	"context"
	"fmt"

	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

// DHT wraps a Kademlia DHT instance for content routing.
type DHT struct {
	host host.Host
	dht  *kaddht.IpfsDHT
}

// NewDHT creates a KadDHT in server mode attached to the given host.
func NewDHT(ctx context.Context, h host.Host) (*DHT, error) {
	d, err := kaddht.New(ctx, h, kaddht.Mode(kaddht.ModeServer))
	if err != nil {
		return nil, fmt.Errorf("create kad-dht: %w", err)
	}
	return &DHT{host: h, dht: d}, nil
}

// Bootstrap connects to the default IPFS bootstrap peers and waits for routing table to fill.
func (d *DHT) Bootstrap(ctx context.Context) error {
	if err := d.dht.Bootstrap(ctx); err != nil {
		return fmt.Errorf("dht bootstrap: %w", err)
	}
	return nil
}

// cidFromString converts a plain string key into an IPFS CID using SHA2-256.
func cidFromString(key string) (cid.Cid, error) {
	mh, err := multihash.Sum([]byte(key), multihash.SHA2_256, -1)
	if err != nil {
		return cid.Undef, fmt.Errorf("multihash: %w", err)
	}
	return cid.NewCidV1(cid.Raw, mh), nil
}

// Provide announces to the DHT that this node has the content identified by cidStr.
func (d *DHT) Provide(ctx context.Context, cidStr string) error {
	c, err := cidFromString(cidStr)
	if err != nil {
		return err
	}
	if err := d.dht.Provide(ctx, c, true); err != nil {
		return fmt.Errorf("dht provide %s: %w", cidStr, err)
	}
	return nil
}

// IpfsDHT returns the underlying *kaddht.IpfsDHT for use by the discovery package.
func (d *DHT) IpfsDHT() *kaddht.IpfsDHT {
	return d.dht
}

// FindProviders queries the DHT for peers that have the content identified by cidStr.
func (d *DHT) FindProviders(ctx context.Context, cidStr string) ([]peer.AddrInfo, error) {
	c, err := cidFromString(cidStr)
	if err != nil {
		return nil, err
	}
	providers, err := d.dht.FindProviders(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("dht find providers %s: %w", cidStr, err)
	}
	return providers, nil
}
