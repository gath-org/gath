package p2p

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const dnsSeedTimeout = 5 * time.Second

// MainnetBootnodes are "host:port" peers a mainnet node dials on startup.
// The first public seed started at genesis. It may have few or no peers.
// --connect still adds peers alongside this list.
var MainnetBootnodes = []string{
	"104.207.90.38:9733",
}

// MainnetDNSSeeds are DNS names. A mainnet node resolves each name and dials
// every A and AAAA record on the mainnet peer port. Empty until a seed name
// is published.
var MainnetDNSSeeds []string

// LookupIP resolves a DNS seed name to IP addresses.
type LookupIP func(ctx context.Context, host string) ([]net.IP, error)

// SystemLookup uses the system resolver.
func SystemLookup(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// ResolveSeeds turns bootnode addresses and DNS seed names into peers to dial.
// A malformed bootnode or DNS name is an error: the list is compiled into the
// binary, so a bad entry should fail startup. A DNS lookup failure is returned
// in dnsErrs and does not fail startup; the node still dials --connect and any
// names that resolved.
func ResolveSeeds(ctx context.Context, bootnodes, dnsSeeds []string, port int, lookup LookupIP) (peers []string, dnsErrs []error, err error) {
	for _, addr := range bootnodes {
		if err := validatePeerAddr(addr); err != nil {
			return nil, nil, err
		}
		peers = appendUnique(peers, addr)
	}
	if len(dnsSeeds) == 0 {
		return peers, nil, nil
	}
	if port < 1 || port > 65535 {
		return nil, nil, fmt.Errorf("dns seed port %d", port)
	}
	if lookup == nil {
		lookup = SystemLookup
	}
	for _, name := range dnsSeeds {
		if err := validateDNSSeed(name); err != nil {
			return nil, nil, err
		}
		lctx, cancel := context.WithTimeout(ctx, dnsSeedTimeout)
		ips, lerr := lookup(lctx, name)
		cancel()
		if lerr != nil {
			dnsErrs = append(dnsErrs, fmt.Errorf("%s: %w", name, lerr))
			continue
		}
		added := 0
		for _, ip := range ips {
			if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
				continue
			}
			peers = appendUnique(peers, net.JoinHostPort(ip.String(), strconv.Itoa(port)))
			added++
		}
		if added == 0 {
			dnsErrs = append(dnsErrs, fmt.Errorf("%s: no usable addresses", name))
		}
	}
	return peers, dnsErrs, nil
}

func validatePeerAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bootnode %q: %w", addr, err)
	}
	if host == "" {
		return fmt.Errorf("bootnode %q: host is required", addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("bootnode %q: port must be 1-65535", addr)
	}
	return nil
}

func validateDNSSeed(name string) error {
	if name == "" || name != strings.TrimSpace(name) {
		return fmt.Errorf("dns seed %q: invalid name", name)
	}
	if _, _, err := net.SplitHostPort(name); err == nil {
		return fmt.Errorf("dns seed %q: use a name, not host:port", name)
	}
	return nil
}

func appendUnique(dst []string, addr string) []string {
	for _, have := range dst {
		if have == addr {
			return dst
		}
	}
	return append(dst, addr)
}
