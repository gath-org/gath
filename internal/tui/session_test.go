package tui

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gath-org/gath/internal/client"
	"github.com/gath-org/gath/internal/node"
)

func TestSessionSwitchesOffMainnetAndBack(t *testing.T) {
	s := NewSession(Options{
		Network: "mainnet",
		Home:    t.TempDir(),
		Listen:  "127.0.0.1:0",
		RPC:     "127.0.0.1:0",
		Threads: 1,
	})
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	snap, err := s.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Network != "mainnet" || snap.Status.Network != "mainnet" || snap.Attached || !snap.Owned {
		t.Fatalf("%+v status %+v", snap, snap.Status)
	}
	if !strings.HasPrefix(snap.RPC, "127.0.0.1:") || snap.RPC == "" {
		t.Fatalf("rpc %s", snap.RPC)
	}
	dev, err := s.Switch(ctx, "devnet")
	if err != nil {
		t.Fatal(err)
	}
	if dev.Network != "devnet" || dev.Status.Network != "devnet" || !dev.Owned {
		t.Fatalf("%+v", dev.Status)
	}
	back, err := s.Switch(ctx, "mainnet")
	if err != nil {
		t.Fatal(err)
	}
	if back.Network != "mainnet" || back.Status.Network != "mainnet" {
		t.Fatalf("%+v", back.Status)
	}
	polled := s.Poll(ctx)
	if polled.Status.Network != "mainnet" || polled.NodeErr != "" {
		t.Fatalf("%+v %s", polled.Status, polled.NodeErr)
	}
}

func TestSessionAttachesAndDoesNotCloseForeignNode(t *testing.T) {
	ctx := context.Background()
	foreign := startQuiet(t, node.Config{
		Network: "devnet",
		DataDir: t.TempDir(),
		Listen:  "127.0.0.1:0",
		RPC:     "127.0.0.1:0",
	})
	s := NewSession(Options{
		Network: "devnet",
		Home:    t.TempDir(),
		RPC:     foreign.RPCAddr(),
		Threads: 1,
	})
	snap, err := s.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Attached || snap.Owned || snap.Status.Network != "devnet" {
		t.Fatalf("attached %v owned %v %+v", snap.Attached, snap.Owned, snap.Status)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.New("http://" + foreign.RPCAddr()).Status(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRejectsForeignNetworkOnPort(t *testing.T) {
	foreign := startQuiet(t, node.Config{
		Network: "devnet",
		DataDir: t.TempDir(),
		Listen:  "127.0.0.1:0",
		RPC:     "127.0.0.1:0",
	})
	s := NewSession(Options{
		Network: "mainnet",
		Home:    t.TempDir(),
		RPC:     foreign.RPCAddr(),
		Listen:  "127.0.0.1:0",
		Threads: 1,
	})
	t.Cleanup(func() { _ = s.Close() })
	_, err := s.Open(context.Background())
	if err == nil || !strings.Contains(err.Error(), "is devnet") {
		t.Fatal(err)
	}
}

func TestSessionMinerPaysWallet(t *testing.T) {
	s := NewSession(Options{
		Network: "devnet",
		Home:    t.TempDir(),
		Listen:  "127.0.0.1:0",
		RPC:     "127.0.0.1:0",
		Threads: 1,
	})
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	if _, err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	phrase, addr, err := s.CreateWallet("test-pass-1")
	if err != nil {
		t.Fatal(err)
	}
	if phrase == "" || !strings.HasPrefix(addr, "gathd") {
		t.Fatalf("phrase %q addr %s", phrase, addr)
	}
	if _, err := s.StartMiner(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if s.miner.Status().Blocks >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s.miner.Status().Blocks < 1 {
		t.Fatalf("no block: %+v", s.miner.Status())
	}
	stopped := s.StopMiner()
	if stopped.Running {
		t.Fatal("miner still running")
	}
	snap := s.Poll(ctx)
	if snap.Status.Height < 1 || snap.Status.Network != "devnet" {
		t.Fatalf("%+v", snap.Status)
	}
	if snap.Wallet.Address != addr {
		t.Fatalf("address %s", snap.Wallet.Address)
	}
	if snap.Wallet.Balance.Confirmed == "" || snap.Wallet.Balance.Confirmed == "0" {
		t.Fatalf("block reward was not paid to the wallet: %+v", snap.Wallet.Balance)
	}
}

func startQuiet(t *testing.T, cfg node.Config) *node.Node {
	t.Helper()
	n, err := node.Start(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	return n
}
