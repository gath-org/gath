package tui

import (
	"testing"

	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/node"
)

func TestDefaultNetworkIsMainnet(t *testing.T) {
	if DefaultNetwork != "mainnet" {
		t.Fatalf("default %s", DefaultNetwork)
	}
	if DefaultNetwork != consensus.Mainnet.String() {
		t.Fatalf("default %s is not the consensus mainnet name", DefaultNetwork)
	}
	if got := Networks(); len(got) != 2 || got[0] != "mainnet" || got[1] != "devnet" {
		t.Fatalf("networks %v", got)
	}
	for _, name := range Networks() {
		if err := ValidateNetwork(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateNetwork("testnet"); err == nil {
		t.Fatal("testnet should be rejected")
	}
}

func TestEndpointsMatchPublishedPorts(t *testing.T) {
	mainRPC, err := RPCBase("mainnet")
	if err != nil {
		t.Fatal(err)
	}
	if mainRPC != "http://127.0.0.1:9734" {
		t.Fatalf("mainnet api %s", mainRPC)
	}
	if node.DefaultRPCListen(consensus.Mainnet) != "127.0.0.1:9734" {
		t.Fatal(node.DefaultRPCListen(consensus.Mainnet))
	}
	devRPC, err := RPCBase("devnet")
	if err != nil {
		t.Fatal(err)
	}
	if devRPC != "http://127.0.0.1:19734" {
		t.Fatalf("devnet api %s", devRPC)
	}
	mainListen, err := ListenAddr("mainnet")
	if err != nil {
		t.Fatal(err)
	}
	if mainListen != "0.0.0.0:9733" {
		t.Fatalf("mainnet listen %s", mainListen)
	}
	devListen, err := ListenAddr("devnet")
	if err != nil {
		t.Fatal(err)
	}
	if devListen != "127.0.0.1:19733" {
		t.Fatalf("devnet listen %s", devListen)
	}
}

func TestNodeConfigKeepsNetworkDefaults(t *testing.T) {
	cfg, err := NodeConfig("mainnet", "/tmp/gath-mainnet", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Network != "mainnet" || cfg.Listen != "" || cfg.RPC != "" || cfg.Mine {
		t.Fatalf("%+v", cfg)
	}
	boot, dns := node.SeedsFor(consensus.Mainnet)
	if len(cfg.Seeds) != len(boot) || len(cfg.DNSSeeds) != len(dns) {
		t.Fatalf("seeds %v dns %v", cfg.Seeds, cfg.DNSSeeds)
	}
	for i := range boot {
		if cfg.Seeds[i] != boot[i] {
			t.Fatalf("seed %s", cfg.Seeds[i])
		}
	}
	dev, err := NodeConfig("devnet", "/tmp/gath-devnet", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if dev.Network != "devnet" || len(dev.Seeds) != 0 || len(dev.DNSSeeds) != 0 || dev.Mine {
		t.Fatalf("%+v", dev)
	}
	data, wallet, err := Layout("/var/gath", "devnet")
	if err != nil {
		t.Fatal(err)
	}
	if data != "/var/gath/devnet" || wallet != "/var/gath/devnet/wallet.json" {
		t.Fatalf("data %s wallet %s", data, wallet)
	}
}

func TestFormatHashrate(t *testing.T) {
	if got := FormatHashrate(0, false); got != "0 H/s" {
		t.Fatal(got)
	}
	if got := FormatHashrate(12, true); got != "measuring" {
		t.Fatal(got)
	}
	if got := FormatHashrate(3.5e6, false); got != "3.50 MH/s" {
		t.Fatal(got)
	}
}
