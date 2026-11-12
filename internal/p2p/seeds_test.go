package p2p

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestResolveSeedsEmptyKeepsNothing(t *testing.T) {
	peers, dnsErrs, err := ResolveSeeds(context.Background(), nil, nil, 0, func(context.Context, string) ([]net.IP, error) {
		t.Fatal("lookup called with no dns seeds")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 || len(dnsErrs) != 0 {
		t.Fatalf("peers %v dnsErrs %v", peers, dnsErrs)
	}
}

func TestResolveSeedsBootnodesAndDNS(t *testing.T) {
	lookup := func(context.Context, string) ([]net.IP, error) {
		return []net.IP{
			net.ParseIP("203.0.113.8"),
			net.ParseIP("2001:db8::5"),
			net.ParseIP("0.0.0.0"),
			net.ParseIP("169.254.1.1"),
			net.ParseIP("203.0.113.8"),
			nil,
		}, nil
	}
	peers, dnsErrs, err := ResolveSeeds(context.Background(), []string{
		"198.51.100.10:9733",
		"198.51.100.10:9733",
		"[2001:db8::1]:9733",
	}, []string{"seed.example"}, 9733, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if len(dnsErrs) != 0 {
		t.Fatal(dnsErrs)
	}
	want := []string{
		"198.51.100.10:9733",
		"[2001:db8::1]:9733",
		"203.0.113.8:9733",
		"[2001:db8::5]:9733",
	}
	if strings.Join(peers, ",") != strings.Join(want, ",") {
		t.Fatalf("peers %v", peers)
	}
}

func TestResolveSeedsDNSFailureIsSoft(t *testing.T) {
	peers, dnsErrs, err := ResolveSeeds(context.Background(), []string{"198.51.100.10:9733"}, []string{"missing.example"}, 9733, func(context.Context, string) ([]net.IP, error) {
		return nil, errors.New("dns down")
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0] != "198.51.100.10:9733" {
		t.Fatalf("peers %v", peers)
	}
	if len(dnsErrs) != 1 || !strings.Contains(dnsErrs[0].Error(), "missing.example") {
		t.Fatalf("dnsErrs %v", dnsErrs)
	}
}

func TestResolveSeedsRejectsBadEntries(t *testing.T) {
	cases := []struct {
		boot []string
		dns  []string
		port int
	}{
		{boot: []string{"not-an-address"}, port: 9733},
		{boot: []string{":9733"}, port: 9733},
		{boot: []string{"203.0.113.1:0"}, port: 9733},
		{boot: []string{"203.0.113.1:99999"}, port: 9733},
		{dns: []string{"seed.example:9733"}, port: 9733},
		{dns: []string{"  seed.example"}, port: 9733},
		{dns: []string{""}, port: 9733},
		{dns: []string{"seed.example"}, port: 0},
	}
	for _, tc := range cases {
		_, _, err := ResolveSeeds(context.Background(), tc.boot, tc.dns, tc.port, func(context.Context, string) ([]net.IP, error) {
			t.Fatal("lookup called for an invalid seed")
			return nil, nil
		})
		if err == nil {
			t.Fatalf("accepted boot %v dns %v port %d", tc.boot, tc.dns, tc.port)
		}
	}
}

func TestPublishedSeedListsAreAddressesNotPlaceholders(t *testing.T) {
	// A later edit adds a real host:port or DNS name. This only checks that
	// whatever is published is well formed, and that an empty list resolves.
	if _, dnsErrs, err := ResolveSeeds(context.Background(), MainnetBootnodes, nil, 9733, nil); err != nil || len(dnsErrs) != 0 {
		t.Fatalf("bootnodes %v err %v dns %v", MainnetBootnodes, err, dnsErrs)
	}
	for _, name := range MainnetDNSSeeds {
		if err := validateDNSSeed(name); err != nil {
			t.Fatal(err)
		}
	}
}
