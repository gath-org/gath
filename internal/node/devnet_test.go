package node

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"

	"github.com/gath-org/gath/internal/address"
	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/keys"
)

func TestMineTransferReorg(t *testing.T) {
	ctx := context.Background()
	n1 := startDev(t, "a")
	n2 := startDev(t, "b")
	n3 := startDev(t, "c")
	if err := n2.Connect(ctx, n1.P2PAddr()); err != nil {
		t.Fatal(err)
	}
	if err := n3.Connect(ctx, n1.P2PAddr()); err != nil {
		t.Fatal(err)
	}
	wait(t, 10*time.Second, func() bool {
		return n1.PeerCount() >= 2 && n2.PeerCount() >= 1 && n3.PeerCount() >= 1
	})

	ka := mustKey(t, 1)
	kb := mustKey(t, 2)
	kc := mustKey(t, 3)
	if err := n1.Generate(ctx, ka.PKH(), int(consensus.CoinbaseMaturity)); err != nil {
		t.Fatal(err)
	}
	wait(t, 20*time.Second, func() bool {
		return n2.ChainHeight() == consensus.CoinbaseMaturity && n3.ChainHeight() == consensus.CoinbaseMaturity
	})

	const sent = 100_000_000
	tx := mustPay(t, n1, ka, kb.PKH(), sent)
	if err := n1.SubmitTx(tx); err != nil {
		t.Fatal(err)
	}
	if n1.MempoolLen() != 1 {
		t.Fatalf("mempool %d", n1.MempoolLen())
	}
	if err := n1.Generate(ctx, ka.PKH(), 1); err != nil {
		t.Fatal(err)
	}
	wait(t, 20*time.Second, func() bool {
		info, err := n3.LookupAddress(mustAddr(t, kb))
		return err == nil && info.ConfirmedAtoms == fmt.Sprintf("%d", sent)
	})

	n1.PauseDial(true)
	n2.PauseDial(true)
	n3.PauseDial(true)
	n3.DisconnectAll()
	wait(t, 10*time.Second, func() bool { return n3.PeerCount() == 0 })

	const back = 25_000_000
	spend := mustPay(t, n1, kb, kc.PKH(), back)
	if err := n1.SubmitTx(spend); err != nil {
		t.Fatal(err)
	}
	if err := n1.Generate(ctx, ka.PKH(), 1); err != nil {
		t.Fatal(err)
	}
	if _, height := n1.Tip(); height != consensus.CoinbaseMaturity+2 {
		t.Fatalf("n1 height %d", height)
	}
	if err := n3.Generate(ctx, ka.PKH(), 4); err != nil {
		t.Fatal(err)
	}
	heavy, heavyHeight := n3.Tip()
	if heavyHeight != consensus.CoinbaseMaturity+1+4 {
		t.Fatalf("n3 height %d", heavyHeight)
	}

	if err := n3.Connect(ctx, n1.P2PAddr()); err != nil {
		t.Fatal(err)
	}
	wait(t, 20*time.Second, func() bool {
		t1, h1 := n1.Tip()
		t2, h2 := n2.Tip()
		return t1 == heavy && t2 == heavy && h1 == heavyHeight && h2 == heavyHeight
	})
	gotC, err := n1.LookupAddress(mustAddr(t, kc))
	if err != nil {
		t.Fatal(err)
	}
	if gotC.ConfirmedAtoms != "0" {
		t.Fatalf("C kept coins across the reorg: %+v", gotC)
	}
	gotB, err := n1.LookupAddress(mustAddr(t, kb))
	if err != nil {
		t.Fatal(err)
	}
	if gotB.ConfirmedAtoms != fmt.Sprintf("%d", sent) {
		t.Fatalf("B balance after reorg %+v", gotB)
	}
	if _, ok, err := n1.chain.FindTx(spend.ID()); err != nil || ok {
		t.Fatalf("reorged spend still in the canonical chain (ok %v err %v)", ok, err)
	}
	if _, ok, err := n2.chain.FindTx(tx.ID()); err != nil || !ok {
		t.Fatal("the transfer was lost in the reorg")
	}
}

func startDev(t *testing.T, _ string) *Node {
	t.Helper()
	n, err := Start(context.Background(), Config{
		Network: "devnet",
		DataDir: t.TempDir(),
		Listen:  "127.0.0.1:0",
		RPC:     "127.0.0.1:0",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	return n
}

func mustKey(t *testing.T, mark byte) *keys.Key {
	t.Helper()
	seed := make([]byte, 64)
	seed[0] = mark
	key, err := keys.Derive(seed, 0)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustAddr(t *testing.T, key *keys.Key) string {
	t.Helper()
	addr, err := address.Encode(consensus.ByNetwork(consensus.Devnet).HRP, key.PKH())
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func mustPay(t *testing.T, n *Node, from *keys.Key, to consensus.Hash, amount uint64) *consensus.Tx {
	t.Helper()
	info, err := n.LookupAddress(mustAddr(t, from))
	if err != nil {
		t.Fatal(err)
	}
	var picked *UTXO
	var value uint64
	for i := range info.UTXOs {
		u := &info.UTXOs[i]
		if !u.Mature || u.Reserved {
			continue
		}
		atoms := parseU64(t, u.Atoms)
		if atoms > value {
			value = atoms
			picked = u
		}
	}
	if picked == nil || value <= amount+consensus.MinOutputAtoms {
		t.Fatalf("no spendable coin for %d (have %d)", amount, value)
	}
	id, err := consensus.ParseHash(picked.TxID)
	if err != nil {
		t.Fatal(err)
	}
	op := consensus.Outpoint{Hash: id, Index: picked.Index}
	change := value - amount - 8000
	tx := &consensus.Tx{
		Version:  consensus.TxVersion,
		LockTime: consensus.LockTimeMust,
		Inputs: []consensus.TxIn{{
			Prev:      op,
			Sequence:  consensus.SequenceMust,
			PubKey:    from.PublicKey(),
			Signature: make([]byte, mldsa44.SignatureSize),
		}},
		Outputs: []consensus.TxOut{
			{Value: amount, PKH: to},
			{Value: change, PKH: from.PKH()},
		},
	}
	need := uint64(len(tx.Bytes()))
	if 8000 < need {
		t.Fatalf("fee budget 8000 < size %d", need)
	}
	msg := consensus.Sighash(tx, 0, value, from.PKH(), n.params.GenesisHash())
	sig, err := from.Sign(msg[:])
	if err != nil {
		t.Fatal(err)
	}
	tx.Inputs[0].Signature = sig
	return tx
}

func wait(t *testing.T, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func parseU64(t *testing.T, s string) uint64 {
	t.Helper()
	var n uint64
	for _, r := range s {
		if r < '0' || r > '9' {
			t.Fatalf("atoms %q", s)
		}
		n = n*10 + uint64(r-'0')
	}
	return n
}
