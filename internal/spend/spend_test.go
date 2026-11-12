package spend

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gath-org/gath/internal/client"
	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/wallet"
)

type fakeNode struct {
	network string
	fee     uint64
}

func (f fakeNode) Status(context.Context) (client.Status, error) {
	return client.Status{Network: f.network, Height: 1}, nil
}

func (f fakeNode) Address(context.Context, string) (client.Address, error) {
	return client.Address{}, nil
}

func (f fakeNode) FeeRate(context.Context) (uint64, error) {
	if f.fee == 0 {
		return consensus.MinFeePerByte, nil
	}
	return f.fee, nil
}

func (f fakeNode) SubmitTx(context.Context, *consensus.Tx) (string, error) {
	return "unused", nil
}

func TestSendRejectsMismatchedNetwork(t *testing.T) {
	w := mustWallet(t, "mainnet")
	_, err := Send(context.Background(), w, fakeNode{network: "devnet"}, "not-an-address", "1")
	if err == nil || !strings.Contains(err.Error(), "wallet is mainnet and the node is devnet") {
		t.Fatal(err)
	}
}

func TestSendRejectsBadAmountBeforeNetworkWork(t *testing.T) {
	w := mustWallet(t, "mainnet")
	_, err := Send(context.Background(), w, fakeNode{network: "mainnet"}, "gath1qqqq", "nope")
	if err == nil || !strings.Contains(err.Error(), "invalid amount") {
		t.Fatal(err)
	}
}

func mustWallet(t *testing.T, network string) *wallet.Wallet {
	t.Helper()
	_, w, err := wallet.Create(filepath.Join(t.TempDir(), "wallet.json"), network, "test-pass-1")
	if err != nil {
		t.Fatal(err)
	}
	return w
}
