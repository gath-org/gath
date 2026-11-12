package main

import (
	"bytes"
	"testing"

	"github.com/gath-org/gath/internal/tui"
)

func TestNoArgsOpensMainnetTUI(t *testing.T) {
	cmd, rest := parseArgs(nil)
	if cmd != "tui" || len(rest) != 0 {
		t.Fatalf("cmd %q rest %v", cmd, rest)
	}
	network, err := tuiNetwork(nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if network != "mainnet" {
		t.Fatalf("network %s", network)
	}
	if tui.DefaultNetwork != "mainnet" {
		t.Fatalf("default %s", tui.DefaultNetwork)
	}
}

func TestTUINetworkFlag(t *testing.T) {
	network, err := tuiNetwork([]string{"--network", "devnet"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if network != "devnet" {
		t.Fatalf("network %s", network)
	}
	var buf bytes.Buffer
	if _, err := tuiNetwork([]string{"--network", "testnet"}, &buf); err == nil {
		t.Fatal("expected an error")
	}
}

func TestScriptedCommandsStay(t *testing.T) {
	for _, name := range []string{"node", "miner", "wallet", "version"} {
		cmd, rest := parseArgs([]string{name, "--network", "devnet"})
		if cmd != name {
			t.Fatalf("%s became %s", name, cmd)
		}
		if len(rest) != 2 || rest[0] != "--network" {
			t.Fatalf("%s rest %v", name, rest)
		}
	}
	if cmd, _ := parseArgs([]string{"help"}); cmd != "help" {
		t.Fatal(cmd)
	}
	if cmd, _ := parseArgs([]string{"--help"}); cmd != "help" {
		t.Fatal(cmd)
	}
	if cmd, _ := parseArgs([]string{"nope"}); cmd != "" {
		t.Fatal(cmd)
	}
}
