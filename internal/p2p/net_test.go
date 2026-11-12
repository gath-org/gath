package p2p

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gath-org/gath/internal/consensus"
)

func TestAdvertisedAddr(t *testing.T) {
	cases := []struct {
		external, bound, want string
	}{
		{"203.0.113.5:9733", "0.0.0.0:9733", "203.0.113.5:9733"},
		{"", "0.0.0.0:9733", ""},
		{"", "[::]:9733", ""},
		{"", ":9733", ""},
		{"", "127.0.0.1:19733", "127.0.0.1:19733"},
		{"", "192.0.2.10:9733", "192.0.2.10:9733"},
	}
	for _, tc := range cases {
		got := advertisedAddr(tc.external, tc.bound)
		if got != tc.want {
			t.Fatalf("advertisedAddr(%q, %q) = %q, want %q", tc.external, tc.bound, got, tc.want)
		}
	}
}

func TestListenAdvertisement(t *testing.T) {
	pub, err := Start(context.Background(), Config{Magic: 1, Listen: "0.0.0.0:0"}, stubHandler{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pub.Close() })
	if !unspecifiedAddr(pub.Addr()) {
		t.Fatalf("bound %s", pub.Addr())
	}
	if pub.External() != "" {
		t.Fatalf("advertised unspecified address %q", pub.External())
	}

	ext, err := Start(context.Background(), Config{Magic: 1, Listen: "0.0.0.0:0", External: "203.0.113.5:9733"}, stubHandler{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ext.Close() })
	if ext.External() != "203.0.113.5:9733" {
		t.Fatalf("external %q", ext.External())
	}

	local, err := Start(context.Background(), Config{Magic: 1, Listen: "127.0.0.1:0"}, stubHandler{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = local.Close() })
	if !strings.HasPrefix(local.External(), "127.0.0.1:") {
		t.Fatalf("loopback advertisement %q", local.External())
	}
}

type stubHandler struct{}

func (stubHandler) ChainHeight() uint64                { return 0 }
func (stubHandler) Genesis() consensus.Hash            { return consensus.Hash{} }
func (stubHandler) HasBlock(consensus.Hash) bool       { return false }
func (stubHandler) Locator() []consensus.Hash          { return nil }
func (stubHandler) HandleBlock(*consensus.Block) error { return nil }
func (stubHandler) HandleTx(*consensus.Tx) error       { return nil }
func (stubHandler) BlocksAfter([]consensus.Hash, consensus.Hash, int) []consensus.Hash {
	return nil
}
func (stubHandler) Block(consensus.Hash) (*consensus.Block, bool) { return nil, false }
func (stubHandler) Tx(consensus.Hash) (*consensus.Tx, bool)       { return nil, false }
func (stubHandler) NoteAddrs([]string)                            {}

func TestRoutableAddr(t *testing.T) {
	ok := []string{"203.0.113.8:9733", "[2001:db8::5]:9733", "8.8.8.8:9733"}
	bad := []string{
		"127.0.0.1:9733",
		"[::1]:9733",
		"10.1.1.1:9733",
		"192.168.1.1:9733",
		"172.16.0.1:9733",
		"169.254.1.1:9733",
		"0.0.0.0:9733",
		"[::]:9733",
		"224.0.0.1:9733",
		"[fc00::1]:9733",
		"seed.example:9733",
		"203.0.113.8:0",
		"not-an-address",
		"",
	}
	for _, addr := range ok {
		if !routableAddr(addr) {
			t.Fatalf("%s should be routable", addr)
		}
	}
	for _, addr := range bad {
		if routableAddr(addr) {
			t.Fatalf("%s should not be routable", addr)
		}
	}
}

func TestLearnedAddressesIgnoreLocalAndEphemeral(t *testing.T) {
	ln, err := Start(context.Background(), Config{
		Magic:    1,
		Listen:   "0.0.0.0:0",
		External: "203.0.113.50:9733",
	}, stubHandler{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	_, port, err := net.SplitHostPort(ln.Addr())
	if err != nil {
		t.Fatal(err)
	}
	dialAddr := net.JoinHostPort("127.0.0.1", port)
	dialer, err := Start(context.Background(), Config{
		Magic:  1,
		Listen: "127.0.0.1:0",
		Pinned: []string{dialAddr},
	}, stubHandler{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dialer.Close() })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ln.PeerCount() == 1 && dialer.PeerCount() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ln.PeerCount() != 1 || dialer.PeerCount() != 1 {
		t.Fatalf("peers listener %d dialer %d", ln.PeerCount(), dialer.PeerCount())
	}
	dialer.dialPinned()
	time.Sleep(50 * time.Millisecond)
	if ln.PeerCount() != 1 || dialer.PeerCount() != 1 {
		t.Fatalf("redial changed peers listener %d dialer %d", ln.PeerCount(), dialer.PeerCount())
	}
	dialer.mu.Lock()
	_, tracked := dialer.byAddr[dialAddr]
	dialer.mu.Unlock()
	if !tracked {
		t.Fatal("dial address was not remembered")
	}
	got := knownAddrs(dialer)
	if len(got) != 1 || got[0] != "203.0.113.50:9733" {
		t.Fatalf("dialer known %v", got)
	}
	if learned := knownAddrs(ln); len(learned) != 0 {
		t.Fatalf("listener learned %v", learned)
	}
}

func knownAddrs(n *Network) []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]string, 0, len(n.known))
	for addr := range n.known {
		out = append(out, addr)
	}
	return out
}

func unspecifiedAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}
