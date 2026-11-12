package chain

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"

	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/keys"
	"github.com/gath-org/gath/internal/miner"
)

func openDev(t *testing.T) (*Chain, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "chain.db")
	c, err := Open(path, consensus.ByNetwork(consensus.Devnet))
	if err != nil {
		t.Fatal(err)
	}
	p := c.Params()
	c.SetClock(func() time.Time {
		return time.Unix(int64(p.GenesisTime)+10_000_000, 0)
	})
	return c, path
}

func mineAt(t *testing.T, c *Chain, parent, pay consensus.Hash, ts uint64, txs []*consensus.Tx, fees uint64) (*consensus.Block, Accept) {
	t.Helper()
	block, err := c.BuildOn(parent, pay, ts, txs, fees)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := miner.Solve(ctx, block, 1); err != nil {
		t.Fatal(err)
	}
	acc, err := c.Process(block)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	return block, acc
}

func coinbaseOf(block *consensus.Block) consensus.Outpoint {
	return consensus.Outpoint{Hash: block.Txs[0].ID(), Index: 0}
}

func mustUTXO(t *testing.T, c *Chain, op consensus.Outpoint, want bool) {
	t.Helper()
	_, ok, err := c.LookupUTXO(op)
	if err != nil {
		t.Fatal(err)
	}
	if ok != want {
		t.Fatalf("utxo %s present %v, want %v", op.Hash, ok, want)
	}
}

func TestReorgBothDirections(t *testing.T) {
	c, path := openDev(t)
	p := c.Params()
	base := p.GenesisTime
	var payA, payB consensus.Hash
	payA[0] = 1
	payB[0] = 2

	a1, acc := mineAt(t, c, p.GenesisHash(), payA, base+45, nil, 0)
	if !acc.TipChanged || acc.Height != 1 {
		t.Fatalf("a1 %+v", acc)
	}
	b1, acc := mineAt(t, c, p.GenesisHash(), payB, base+45, nil, 0)
	if acc.TipChanged {
		t.Fatal("an equal-work sibling replaced the tip")
	}
	tip, _ := c.Tip()
	if tip != a1.Hash() {
		t.Fatal("tip moved for equal work")
	}
	mustUTXO(t, c, coinbaseOf(a1), true)
	mustUTXO(t, c, coinbaseOf(b1), false)

	b2, acc := mineAt(t, c, b1.Hash(), payB, base+90, nil, 0)
	if !acc.TipChanged || acc.Tip != b2.Hash() || len(acc.Removed) != 1 || len(acc.Added) != 2 {
		t.Fatalf("reorg to B: %+v", acc)
	}
	mustUTXO(t, c, coinbaseOf(a1), false)
	mustUTXO(t, c, coinbaseOf(b1), true)
	mustUTXO(t, c, coinbaseOf(b2), true)

	a2, acc := mineAt(t, c, a1.Hash(), payA, base+90, nil, 0)
	if acc.TipChanged {
		t.Fatal("equal cumulative work replaced the tip")
	}
	_ = a2
	a3, acc := mineAt(t, c, a2.Hash(), payA, base+135, nil, 0)
	if acc.Tip != a3.Hash() || acc.Height != 3 {
		t.Fatalf("reorg back to A: %+v", acc)
	}
	mustUTXO(t, c, coinbaseOf(a1), true)
	mustUTXO(t, c, coinbaseOf(a3), true)
	mustUTXO(t, c, coinbaseOf(b2), false)

	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c2, err := Open(path, p)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	tip, height := c2.Tip()
	if tip != a3.Hash() || height != 3 {
		t.Fatalf("reopen tip %s height %d", tip, height)
	}
	mustUTXO(t, c2, coinbaseOf(a3), true)
	mustUTXO(t, c2, coinbaseOf(b1), false)
}

func TestSpendMatureCoinbase(t *testing.T) {
	c, _ := openDev(t)
	defer c.Close()
	p := c.Params()
	seed := make([]byte, 64)
	seed[0] = 3
	key, err := keys.Derive(seed, 0)
	if err != nil {
		t.Fatal(err)
	}
	var parent consensus.Hash
	for h := uint64(1); h <= consensus.CoinbaseMaturity; h++ {
		block, _ := mineAt(t, c, parent, key.PKH(), p.GenesisTime+h*consensus.TargetSpacing, nil, 0)
		parent = block.Hash()
	}
	rows, err := c.UTXOsByAddress(key.PKH())
	if err != nil {
		t.Fatal(err)
	}
	var coin UTXOInfo
	var found bool
	for _, row := range rows {
		if row.UTXO.Height == 1 {
			coin = row
			found = true
		}
	}
	if !found {
		t.Fatal("missing height-1 coinbase")
	}
	const amount = 100_000_000
	tx := signedPayment(t, p, key, coin, amount)
	fee := coin.UTXO.Value - amount - tx.Outputs[1].Value
	bad := *tx
	bad.Inputs = append([]consensus.TxIn(nil), tx.Inputs...)
	bad.Inputs[0].PubKey = append([]byte(nil), tx.Inputs[0].PubKey...)
	bad.Inputs[0].Signature = append([]byte(nil), tx.Inputs[0].Signature...)
	bad.Inputs[0].Signature[len(bad.Inputs[0].Signature)-1] ^= 0xff
	badBlock, err := c.BuildOn(consensus.Hash{}, key.PKH(), p.GenesisTime+101*consensus.TargetSpacing, []*consensus.Tx{&bad}, fee)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := miner.Solve(ctx, badBlock, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Process(badBlock); err == nil {
		t.Fatal("bad signature was accepted")
	}
	tip, height := c.Tip()
	if height != consensus.CoinbaseMaturity {
		t.Fatalf("tip moved after a bad signature: %s %d", tip, height)
	}

	block, err := c.BuildOn(consensus.Hash{}, key.PKH(), p.GenesisTime+101*consensus.TargetSpacing, []*consensus.Tx{tx}, fee)
	if err != nil {
		t.Fatal(err)
	}
	if err := miner.Solve(ctx, block, 1); err != nil {
		t.Fatal(err)
	}
	acc, err := c.Process(block)
	if err != nil {
		t.Fatal(err)
	}
	if !acc.TipChanged || acc.Height != 101 {
		t.Fatalf("spend block %+v", acc)
	}
	_, ok, err := c.LookupUTXO(coin.Outpoint)
	if err != nil || ok {
		t.Fatal("spent coinbase still present", ok, err)
	}
	_, ok, err = c.LookupUTXO(consensus.Outpoint{Hash: tx.ID(), Index: 0})
	if err != nil || !ok {
		t.Fatal("payment output missing")
	}

	inflated, err := c.BuildOn(consensus.Hash{}, key.PKH(), p.GenesisTime+102*consensus.TargetSpacing, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := consensus.NewCoinbase(102, consensus.Subsidy(102)+1, key.PKH(), 0, []byte("gath"))
	if err != nil {
		t.Fatal(err)
	}
	inflated.Txs[0] = cb
	ids := make([]consensus.Hash, len(inflated.Txs))
	for i, tx := range inflated.Txs {
		ids[i] = tx.ID()
	}
	inflated.Header.Merkle = consensus.MerkleRoot(ids)
	inflated.Header.Nonce = 0
	if err := miner.Solve(ctx, inflated, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Process(inflated); err == nil {
		t.Fatal("inflated coinbase was accepted")
	}
}

func signedPayment(t *testing.T, p *consensus.Params, key *keys.Key, coin UTXOInfo, amount uint64) *consensus.Tx {
	t.Helper()
	pub := key.PublicKey()
	change := coin.UTXO.Value - amount - 5000
	tx := &consensus.Tx{
		Version:  consensus.TxVersion,
		LockTime: consensus.LockTimeMust,
		Inputs: []consensus.TxIn{{
			Prev:      coin.Outpoint,
			Sequence:  consensus.SequenceMust,
			PubKey:    pub,
			Signature: make([]byte, mldsa44.SignatureSize),
		}},
		Outputs: []consensus.TxOut{
			{Value: amount, PKH: func() consensus.Hash { var h consensus.Hash; h[0] = 9; return h }()},
			{Value: change, PKH: key.PKH()},
		},
	}
	fee := coin.UTXO.Value - amount - change
	need := uint64(len(tx.Bytes()))
	if fee < need {
		t.Fatalf("fee budget %d size %d", fee, need)
	}
	// Put the spare atoms into the fee so the size check is exact enough.
	// The placeholder signature has the real size, so this length is final.
	msg := consensus.Sighash(tx, 0, coin.UTXO.Value, key.PKH(), p.GenesisHash())
	sig, err := key.Sign(msg[:])
	if err != nil {
		t.Fatal(err)
	}
	tx.Inputs[0].Signature = sig
	if uint64(len(tx.Bytes())) > fee {
		t.Fatalf("signed size %d exceeds fee %d", len(tx.Bytes()), fee)
	}
	return tx
}
