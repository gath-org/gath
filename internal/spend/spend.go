// Package spend builds a wallet payment and submits it to a node.
package spend

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gath-org/gath/internal/address"
	"github.com/gath-org/gath/internal/amount"
	"github.com/gath-org/gath/internal/client"
	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/wallet"
)

// Node is the local API a payment needs.
type Node interface {
	Status(context.Context) (client.Status, error)
	Address(context.Context, string) (client.Address, error)
	FeeRate(context.Context) (uint64, error)
	SubmitTx(context.Context, *consensus.Tx) (string, error)
}

// Send pays amount Gath from w to to and returns the transaction id.
// amount is a decimal Gath string. The node and the wallet must be on the same network.
func Send(ctx context.Context, w *wallet.Wallet, node Node, to, amountText string) (string, error) {
	if w == nil {
		return "", fmt.Errorf("unlock the wallet first")
	}
	if node == nil {
		return "", fmt.Errorf("node is not running")
	}
	atoms, err := amount.Parse(strings.TrimSpace(amountText))
	if err != nil {
		return "", err
	}
	st, err := node.Status(ctx)
	if err != nil {
		return "", err
	}
	if st.Network != w.Network() {
		return "", fmt.Errorf("wallet is %s and the node is %s", w.Network(), st.Network)
	}
	net, err := consensus.ParseNetwork(w.Network())
	if err != nil {
		return "", err
	}
	params := consensus.ByNetwork(net)
	hrp, payTo, err := address.Decode(strings.TrimSpace(to))
	if err != nil {
		return "", err
	}
	if hrp != params.HRP {
		return "", fmt.Errorf("destination address is for %q", hrp)
	}
	from, err := w.Address()
	if err != nil {
		return "", err
	}
	info, err := node.Address(ctx, from)
	if err != nil {
		return "", err
	}
	rate, err := node.FeeRate(ctx)
	if err != nil {
		return "", err
	}
	coins, err := coinsFrom(info)
	if err != nil {
		return "", err
	}
	tx, err := w.BuildPayment(payTo, atoms, rate, st.Height, coins)
	if err != nil {
		return "", err
	}
	return node.SubmitTx(ctx, tx)
}

func coinsFrom(info client.Address) ([]wallet.Coin, error) {
	var coins []wallet.Coin
	for _, u := range info.UTXOs {
		if !u.Mature || u.Reserved {
			continue
		}
		value, err := strconv.ParseUint(u.Atoms, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("atoms: %w", err)
		}
		id, err := consensus.ParseHash(u.TxID)
		if err != nil {
			return nil, err
		}
		coins = append(coins, wallet.Coin{
			Outpoint: consensus.Outpoint{Hash: id, Index: u.Index},
			Value:    value,
			Height:   u.Height,
			Coinbase: u.Coinbase,
		})
	}
	return coins, nil
}
