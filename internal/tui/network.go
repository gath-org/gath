package tui

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/node"
)

// DefaultNetwork is the network opened by `gath` with no arguments.
const DefaultNetwork = "mainnet"

// Networks are the networks this binary can run.
// mainnet is first because it is the default.
func Networks() []string {
	return []string{consensus.Mainnet.String(), consensus.Devnet.String()}
}

// DefaultHome is the directory that holds one subdirectory per network.
func DefaultHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New("home directory is required")
	}
	return filepath.Join(home, ".gath"), nil
}

// ValidateNetwork accepts a network this binary already supports.
func ValidateNetwork(name string) error {
	_, err := consensus.ParseNetwork(name)
	return err
}

// RPCBase is the local JSON API for network, using the node's published port.
func RPCBase(network string) (string, error) {
	n, err := consensus.ParseNetwork(network)
	if err != nil {
		return "", err
	}
	return "http://" + node.DefaultRPCListen(n), nil
}

// ListenAddr is the peer address the node uses when no override is set.
func ListenAddr(network string) (string, error) {
	n, err := consensus.ParseNetwork(network)
	if err != nil {
		return "", err
	}
	return node.DefaultP2PListen(n), nil
}

// Layout is the chain directory and wallet file for one network under home.
func Layout(home, network string) (dataDir, walletPath string, err error) {
	if err = ValidateNetwork(network); err != nil {
		return "", "", err
	}
	if home == "" {
		return "", "", errors.New("home directory is required")
	}
	dataDir = filepath.Join(home, network)
	walletPath = filepath.Join(dataDir, "wallet.json")
	return dataDir, walletPath, nil
}

// NodeConfig is a node configuration for the terminal.
// Empty listen and rpc keep the network's published defaults.
// The terminal mines through its own controller, so Mine stays off.
func NodeConfig(network, dataDir, listen, rpc string) (node.Config, error) {
	n, err := consensus.ParseNetwork(network)
	if err != nil {
		return node.Config{}, err
	}
	if dataDir == "" {
		return node.Config{}, errors.New("data directory is required")
	}
	boot, dns := node.SeedsFor(n)
	return node.Config{
		Network:  n.String(),
		DataDir:  dataDir,
		Listen:   listen,
		RPC:      rpc,
		Seeds:    boot,
		DNSSeeds: dns,
	}, nil
}
