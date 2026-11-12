package node

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/gath-org/gath/internal/consensus"
)

// DefaultP2PListen is the peer address used when --listen is omitted.
// Mainnet accepts inbound connections on every IPv4 interface. Devnet stays
// on localhost so a local network is not reachable from other machines.
func DefaultP2PListen(n consensus.Network) string {
	return net.JoinHostPort(p2pHost(n), strconv.Itoa(consensus.ByNetwork(n).P2PPort))
}

// DefaultRPCListen is the JSON API address used when --rpc is omitted.
// The API has no authentication, so both networks bind it to localhost.
func DefaultRPCListen(n consensus.Network) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(consensus.ByNetwork(n).RPCPort))
}

func p2pHost(n consensus.Network) string {
	switch n {
	case consensus.Mainnet:
		return "0.0.0.0"
	case consensus.Devnet:
		return "127.0.0.1"
	default:
		panic(fmt.Sprintf("unknown network %d", n))
	}
}

// localAPI reports whether addr is a loopback bind. The JSON API has no
// authentication and must not be reachable from other machines by default.
func localAPI(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
