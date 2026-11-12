package node

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gath-org/gath/internal/consensus"
)

func TestDefaultListenAddresses(t *testing.T) {
	if got := DefaultP2PListen(consensus.Mainnet); got != "0.0.0.0:9733" {
		t.Fatalf("mainnet p2p %s", got)
	}
	if got := DefaultP2PListen(consensus.Devnet); got != "127.0.0.1:19733" {
		t.Fatalf("devnet p2p %s", got)
	}
	if got := DefaultRPCListen(consensus.Mainnet); got != "127.0.0.1:9734" {
		t.Fatalf("mainnet api %s", got)
	}
	if got := DefaultRPCListen(consensus.Devnet); got != "127.0.0.1:19734" {
		t.Fatalf("devnet api %s", got)
	}
}

func TestLocalAPI(t *testing.T) {
	local := []string{"127.0.0.1:9734", "[::1]:9734", "localhost:9734"}
	public := []string{"0.0.0.0:9734", "[::]:9734", ":9734", "192.0.2.10:9734", "example.com:9734", "9734"}
	for _, addr := range local {
		if !localAPI(addr) {
			t.Fatalf("%s should be local", addr)
		}
	}
	for _, addr := range public {
		if localAPI(addr) {
			t.Fatalf("%s should not be local", addr)
		}
	}
}

func TestMainnetDefaultBinds(t *testing.T) {
	n := startNode(t, Config{Network: "mainnet", DataDir: t.TempDir()})
	// Go reports a dual-stack unspecified listen as [::]:port. The address we
	// pass is still 0.0.0.0:9733, and IPv4 clients can connect.
	if _, port, err := net.SplitHostPort(n.P2PAddr()); err != nil || port != "9733" || !unspecified(n.P2PAddr()) {
		t.Fatalf("p2p %s", n.P2PAddr())
	}
	if n.RPCAddr() != DefaultRPCListen(consensus.Mainnet) {
		t.Fatalf("api %s", n.RPCAddr())
	}
	if !localAPI(n.RPCAddr()) {
		t.Fatalf("api %s is not loopback", n.RPCAddr())
	}
	assertNetwork(t, n.RPCAddr(), "mainnet")
	dialOK(t, "127.0.0.1:9733")

	ip := hostIPv4()
	if ip == nil {
		return
	}
	publicP2P := net.JoinHostPort(ip.String(), "9733")
	publicAPI := net.JoinHostPort(ip.String(), "9734")
	dialOK(t, publicP2P)
	if err := dialErr(publicAPI); err == nil {
		t.Fatalf("JSON API accepted a connection on %s", publicAPI)
	}
}

func TestDevnetDefaultStaysLocal(t *testing.T) {
	n := startNode(t, Config{Network: "devnet", DataDir: t.TempDir()})
	if n.P2PAddr() != DefaultP2PListen(consensus.Devnet) {
		t.Fatalf("p2p %s", n.P2PAddr())
	}
	if n.RPCAddr() != DefaultRPCListen(consensus.Devnet) {
		t.Fatalf("api %s", n.RPCAddr())
	}
	if !strings.HasPrefix(n.P2PAddr(), "127.0.0.1:") {
		t.Fatalf("devnet peers are reachable beyond localhost: %s", n.P2PAddr())
	}
	assertNetwork(t, n.RPCAddr(), "devnet")

	ip := hostIPv4()
	if ip == nil {
		return
	}
	if err := dialErr(net.JoinHostPort(ip.String(), "19733")); err == nil {
		t.Fatal("devnet P2P accepted a non-loopback connection")
	}
	if err := dialErr(net.JoinHostPort(ip.String(), "19734")); err == nil {
		t.Fatal("devnet API accepted a non-loopback connection")
	}
}

func TestListenOverride(t *testing.T) {
	n := startNode(t, Config{
		Network: "mainnet",
		DataDir: t.TempDir(),
		Listen:  "127.0.0.1:0",
		RPC:     "127.0.0.1:0",
	})
	if !strings.HasPrefix(n.P2PAddr(), "127.0.0.1:") {
		t.Fatalf("override did not stick: %s", n.P2PAddr())
	}
	if !strings.HasPrefix(n.RPCAddr(), "127.0.0.1:") {
		t.Fatalf("api %s", n.RPCAddr())
	}
}

func TestPublicListenAcceptsInbound(t *testing.T) {
	pub := startNode(t, Config{
		Network: "devnet",
		DataDir: t.TempDir(),
		Listen:  "0.0.0.0:0",
		RPC:     "127.0.0.1:0",
	})
	if !unspecified(pub.P2PAddr()) {
		t.Fatalf("bound %s", pub.P2PAddr())
	}
	_, port, err := net.SplitHostPort(pub.P2PAddr())
	if err != nil {
		t.Fatal(err)
	}
	other := startDev(t, "dialer")
	if err := other.Connect(context.Background(), net.JoinHostPort("127.0.0.1", port)); err != nil {
		t.Fatal(err)
	}
	wait(t, 10*time.Second, func() bool {
		return pub.PeerCount() >= 1 && other.PeerCount() >= 1
	})
}

func startNode(t *testing.T, cfg Config) *Node {
	t.Helper()
	n, err := Start(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	return n
}

func assertNetwork(t *testing.T, addr, network string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body struct {
		Network string `json:"network"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Network != network {
		t.Fatalf("network %s", body.Network)
	}
}

func dialOK(t *testing.T, addr string) {
	t.Helper()
	if err := dialErr(addr); err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
}

func dialErr(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

func unspecified(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

func hostIPv4() net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagRunning == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
				continue
			}
			return ip
		}
	}
	return nil
}
