package node

import (
	"fmt"

	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/p2p"
)

// SeedsFor returns the bootnodes and DNS seeds a network dials on startup.
// Mainnet uses the lists published in the p2p package. Devnet uses none, so
// a local node is not pointed at mainnet peers. --connect is separate and
// works on both networks when these lists are empty.
func SeedsFor(n consensus.Network) (bootnodes, dnsSeeds []string) {
	return selectSeeds(n, p2p.MainnetBootnodes, p2p.MainnetDNSSeeds)
}

func selectSeeds(n consensus.Network, mainnetBoot, mainnetDNS []string) (bootnodes, dnsSeeds []string) {
	switch n {
	case consensus.Mainnet:
		return copyStrings(mainnetBoot), copyStrings(mainnetDNS)
	case consensus.Devnet:
		return nil, nil
	default:
		panic(fmt.Sprintf("unknown network %d", n))
	}
}

func copyStrings(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	return append([]string(nil), src...)
}

func mergePeers(connect, seeds []string) []string {
	out := make([]string, 0, len(connect)+len(seeds))
	seen := make(map[string]struct{}, len(connect)+len(seeds))
	for _, group := range [][]string{connect, seeds} {
		for _, addr := range group {
			if _, ok := seen[addr]; ok {
				continue
			}
			seen[addr] = struct{}{}
			out = append(out, addr)
		}
	}
	return out
}
