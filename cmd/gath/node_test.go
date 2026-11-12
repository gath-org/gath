package main

import (
	"testing"

	"github.com/gath-org/gath/internal/consensus"
)

func TestNodeConfigPassesNetworkToSeedList(t *testing.T) {
	orig := seedsFor
	t.Cleanup(func() { seedsFor = orig })
	seedsFor = func(n consensus.Network) ([]string, []string) {
		switch n {
		case consensus.Mainnet:
			return []string{"198.51.100.20:9733"}, []string{"seed.example"}
		case consensus.Devnet:
			return []string{"devnet-sentinel"}, []string{"devnet-dns"}
		default:
			t.Fatalf("network %d", n)
			return nil, nil
		}
	}

	mainnet := nodeConfig("mainnet", "data", "", "", "", []string{"198.51.100.4:9733"}, false, "", 1)
	if mainnet.Listen != "" || mainnet.RPC != "" {
		t.Fatalf("listen %q rpc %q", mainnet.Listen, mainnet.RPC)
	}
	if len(mainnet.Seeds) != 1 || mainnet.Seeds[0] != "198.51.100.20:9733" {
		t.Fatalf("seeds %v", mainnet.Seeds)
	}
	if len(mainnet.DNSSeeds) != 1 || mainnet.DNSSeeds[0] != "seed.example" {
		t.Fatalf("dns %v", mainnet.DNSSeeds)
	}
	if len(mainnet.Connect) != 1 || mainnet.Connect[0] != "198.51.100.4:9733" {
		t.Fatalf("connect %v", mainnet.Connect)
	}

	// Devnet must ask for the devnet list. selectSeeds drops that list so a
	// filled mainnet seed is not dialed by a local node.
	dev := nodeConfig("devnet", "data", "", "", "", []string{"127.0.0.1:19743"}, false, "", 1)
	if len(dev.Seeds) != 1 || dev.Seeds[0] != "devnet-sentinel" {
		t.Fatalf("devnet seeds %v", dev.Seeds)
	}
	if len(dev.DNSSeeds) != 1 || dev.DNSSeeds[0] != "devnet-dns" {
		t.Fatalf("devnet dns %v", dev.DNSSeeds)
	}
	if len(dev.Connect) != 1 || dev.Connect[0] != "127.0.0.1:19743" {
		t.Fatalf("devnet connect %v", dev.Connect)
	}
}
