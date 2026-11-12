package mempool

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"

	"github.com/gath-org/gath/internal/chain"
	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/keys"
	"github.com/gath-org/gath/internal/miner"
)

func TestConflict(t *testing.T) {
	dir := t.TempDir()
	params := consensus.ByNetwork(consensus.Devnet)
	ch, err := chain.Open(filepath.Join(dir, "chain.db"), params)
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	ch.SetClock(func() time.Time {
		return time.Unix(int64(params.GenesisTime)+10_000_000, 0)
	})
	seed := make([]byte, 64)
	seed[2] = 1
	key, err := keys.Derive(seed, 0)
	if err != nil {
		t.Fatal(err)
	}
	var parent consensus.Hash
	for h := uint64(1); h <= consensus.CoinbaseMaturity; h++ {
		block, err := ch.BuildOn(parent, key.PKH(), params.GenesisTime+h*consensus.TargetSpacing, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := miner.Solve(ctx, block, 1); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		if _, err := ch.Process(block); err != nil {
			t.Fatal(err)
		}
		parent = block.Hash()
	}
	rows, err := ch.UTXOsByAddress(key.PKH())
	if err != nil {
		t.Fatal(err)
	}
	var coin chain.UTXOInfo
	for _, row := range rows {
		if row.UTXO.Height == 1 {
			coin = row
		}
	}
	pool := New(params, ch)
	first := payment(t, params, key, coin, 1)
	second := payment(t, params, key, coin, 2)
	if err := pool.Add(first); err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(first); err != ErrKnown {
		t.Fatalf("second insert: %v", err)
	}
	if err := pool.Add(second); err == nil {
		t.Fatal("conflicting spend was accepted")
	}
	txs, fees := pool.Select(consensus.MaxBlockBytes)
	if len(txs) != 1 || txs[0].ID() != first.ID() || fees == 0 {
		t.Fatalf("select %d fees %d", len(txs), fees)
	}
}

func payment(t *testing.T, p *consensus.Params, key *keys.Key, coin chain.UTXOInfo, mark byte) *consensus.Tx {
	t.Helper()
	var to consensus.Hash
	to[0] = mark
	const amount = 50_000_000
	change := coin.UTXO.Value - amount - 8000
	tx := &consensus.Tx{
		Version:  consensus.TxVersion,
		LockTime: consensus.LockTimeMust,
		Inputs: []consensus.TxIn{{
			Prev:      coin.Outpoint,
			Sequence:  consensus.SequenceMust,
			PubKey:    key.PublicKey(),
			Signature: make([]byte, mldsa44.SignatureSize),
		}},
		Outputs: []consensus.TxOut{
			{Value: amount, PKH: to},
			{Value: change, PKH: key.PKH()},
		},
	}
	msg := consensus.Sighash(tx, 0, coin.UTXO.Value, key.PKH(), p.GenesisHash())
	sig, err := key.Sign(msg[:])
	if err != nil {
		t.Fatal(err)
	}
	tx.Inputs[0].Signature = sig
	return tx
}
