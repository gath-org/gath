package node

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/p2p"
)

func TestSelectSeeds(t *testing.T) {
	boot := []string{"198.51.100.8:9733"}
	dns := []string{"seed.example"}
	gotBoot, gotDNS := selectSeeds(consensus.Mainnet, boot, dns)
	if !sameStrings(gotBoot, boot) || !sameStrings(gotDNS, dns) {
		t.Fatalf("mainnet boot %v dns %v", gotBoot, gotDNS)
	}
	gotBoot[0] = "changed"
	if boot[0] != "198.51.100.8:9733" {
		t.Fatal("seed list was aliased")
	}
	gotBoot, gotDNS = selectSeeds(consensus.Devnet, boot, dns)
	if len(gotBoot) != 0 || len(gotDNS) != 0 {
		t.Fatalf("devnet took mainnet seeds boot %v dns %v", gotBoot, gotDNS)
	}
}

func TestSeedsForUsesPublishedLists(t *testing.T) {
	boot, dns := SeedsFor(consensus.Mainnet)
	if !sameStrings(boot, p2p.MainnetBootnodes) || !sameStrings(dns, p2p.MainnetDNSSeeds) {
		t.Fatalf("mainnet boot %v dns %v published boot %v dns %v", boot, dns, p2p.MainnetBootnodes, p2p.MainnetDNSSeeds)
	}
	boot, dns = SeedsFor(consensus.Devnet)
	if len(boot) != 0 || len(dns) != 0 {
		t.Fatalf("devnet boot %v dns %v", boot, dns)
	}
}

func TestMergePeersPrefersConnect(t *testing.T) {
	got := mergePeers([]string{"127.0.0.1:1", "127.0.0.1:1"}, []string{"127.0.0.1:2", "127.0.0.1:1"})
	if !sameStrings(got, []string{"127.0.0.1:1", "127.0.0.1:2"}) {
		t.Fatalf("merged %v", got)
	}
	if len(mergePeers(nil, nil)) != 0 {
		t.Fatal("empty merge")
	}
}

func TestManualConnectWithEmptySeeds(t *testing.T) {
	a := startDev(t, "a")
	b := startNode(t, Config{
		Network: "devnet",
		DataDir: t.TempDir(),
		Listen:  "127.0.0.1:0",
		RPC:     "127.0.0.1:0",
		Connect: []string{a.P2PAddr()},
	})
	wait(t, 10*time.Second, func() bool {
		return a.PeerCount() >= 1 && b.PeerCount() >= 1
	})
}

func TestBootnodeDialWithoutConnect(t *testing.T) {
	a := startDev(t, "seed")
	b := startNode(t, Config{
		Network: "devnet",
		DataDir: t.TempDir(),
		Listen:  "127.0.0.1:0",
		RPC:     "127.0.0.1:0",
		Seeds:   []string{a.P2PAddr()},
	})
	wait(t, 10*time.Second, func() bool {
		return a.PeerCount() >= 1 && b.PeerCount() >= 1
	})
}

func TestBadBootnodeRejected(t *testing.T) {
	_, err := Start(context.Background(), Config{
		Network: "devnet",
		DataDir: t.TempDir(),
		Listen:  "127.0.0.1:0",
		Seeds:   []string{"not-an-address"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "bootnode") {
		t.Fatalf("err %v", err)
	}
}

func TestDNSSeedFailureStillConnects(t *testing.T) {
	a := startDev(t, "a")
	b := startNode(t, Config{
		Network:  "devnet",
		DataDir:  t.TempDir(),
		Listen:   "127.0.0.1:0",
		RPC:      "127.0.0.1:0",
		Connect:  []string{a.P2PAddr()},
		DNSSeeds: []string{"seed.example"},
		Lookup: func(context.Context, string) ([]net.IP, error) {
			return nil, context.DeadlineExceeded
		},
	})
	wait(t, 10*time.Second, func() bool {
		return a.PeerCount() >= 1 && b.PeerCount() >= 1
	})
}

func TestDNSSeedDialsResolvedAddress(t *testing.T) {
	seed := startNode(t, Config{
		Network: "devnet",
		DataDir: t.TempDir(),
		Listen:  "127.0.0.1:19733",
		RPC:     "127.0.0.1:0",
	})
	b := startNode(t, Config{
		Network:  "devnet",
		DataDir:  t.TempDir(),
		Listen:   "127.0.0.1:0",
		RPC:      "127.0.0.1:0",
		DNSSeeds: []string{"seed.test"},
		Lookup: func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
	})
	wait(t, 10*time.Second, func() bool {
		return seed.PeerCount() >= 1 && b.PeerCount() >= 1
	})
}

func TestDevnetDoesNotResolveDNS(t *testing.T) {
	n := startNode(t, Config{
		Network: "devnet",
		DataDir: t.TempDir(),
		Listen:  "127.0.0.1:0",
		RPC:     "127.0.0.1:0",
		Lookup: func(context.Context, string) ([]net.IP, error) {
			t.Fatal("lookup called")
			return nil, nil
		},
	})
	if n.PeerCount() != 0 {
		t.Fatalf("peers %d", n.PeerCount())
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
