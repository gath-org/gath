package consensus

import (
	"math/big"
	"testing"
)

func TestSupplyCap(t *testing.T) {
	if Subsidy(0) != 0 {
		t.Fatalf("genesis subsidy %d", Subsidy(0))
	}
	if Subsidy(1) != InitialSubsidyAtoms || Subsidy(HalvingInterval) != InitialSubsidyAtoms {
		t.Fatalf("first era subsidy")
	}
	if Subsidy(HalvingInterval+1) != InitialSubsidyAtoms/2 {
		t.Fatalf("first halving got %d", Subsidy(HalvingInterval+1))
	}
	const lastEra = 28
	first := lastEra*HalvingInterval + 1
	last := (lastEra + 1) * HalvingInterval
	if Subsidy(first) != 1 || Subsidy(last) != 1 {
		t.Fatalf("tail subsidy first %d last %d", Subsidy(first), Subsidy(last))
	}
	if Subsidy(last+1) != 0 {
		t.Fatalf("subsidy continues past the tail: %d", Subsidy(last+1))
	}
	var sum uint64
	for era := uint64(0); ; era++ {
		sub := InitialSubsidyAtoms >> era
		if sub == 0 {
			break
		}
		sum += sub * HalvingInterval
	}
	if sum != MaxSupplyAtoms {
		t.Fatalf("supply sum %d, cap constant %d", sum, MaxSupplyAtoms)
	}
}

func TestASERTRelative(t *testing.T) {
	ref := new(big.Int).Lsh(big.NewInt(0xffff), 200)
	limit := new(big.Int).Lsh(big.NewInt(1), 255)
	const spacing, half int64 = 45, 12_960

	same, err := CalcASERT(ref, spacing, spacing, 0, half, limit)
	if err != nil {
		t.Fatal(err)
	}
	if same.Cmp(ref) != 0 {
		t.Fatalf("on-schedule target changed: %s", same.Text(16))
	}
	doubled, err := CalcASERT(ref, spacing, spacing+half, 0, half, limit)
	if err != nil {
		t.Fatal(err)
	}
	want := new(big.Int).Lsh(ref, 1)
	if doubled.Cmp(want) != 0 {
		t.Fatalf("double got %s want %s", doubled.Text(16), want.Text(16))
	}
	halved, err := CalcASERT(ref, spacing, spacing-half, 0, half, limit)
	if err != nil {
		t.Fatal(err)
	}
	want.Rsh(ref, 1)
	if halved.Cmp(want) != 0 {
		t.Fatalf("half got %s want %s", halved.Text(16), want.Text(16))
	}

	p := ByNetwork(Devnet)
	parent := &BlockHeader{Time: p.GenesisTime + TargetSpacing, Bits: p.PowLimitBits}
	gp := &BlockHeader{Time: p.GenesisTime, Bits: p.PowLimitBits}
	bits, err := NextBits(parent, 1, gp, p)
	if err != nil {
		t.Fatal(err)
	}
	if bits != p.PowLimitBits {
		t.Fatalf("scheduled second block bits %08x, pow limit %08x", bits, p.PowLimitBits)
	}
	first, err := NextBits(&BlockHeader{Bits: p.PowLimitBits}, 0, nil, p)
	if err != nil {
		t.Fatal(err)
	}
	if first != p.PowLimitBits {
		t.Fatalf("block above genesis bits %08x", first)
	}
}

func TestCompactBitcoinVector(t *testing.T) {
	// 0x1d00ffff is the historical Bitcoin pow limit: 0xffff << 208.
	got, err := CompactToTarget(0x1d00ffff)
	if err != nil {
		t.Fatal(err)
	}
	want := new(big.Int).Lsh(big.NewInt(0xffff), 208)
	if got.Cmp(want) != 0 {
		t.Fatalf("got %s", got.Text(16))
	}
	if TargetToCompact(want) != 0x1d00ffff {
		t.Fatalf("compact round trip %08x", TargetToCompact(want))
	}
	main, err := CompactToTarget(MainnetPowBits)
	if err != nil {
		t.Fatal(err)
	}
	pow229 := new(big.Int).Lsh(big.NewInt(1), 229)
	if main.Cmp(pow229) != 0 {
		t.Fatalf("mainnet pow limit %s", main.Text(16))
	}
	if _, err := CompactToTarget(0x1d800000); err == nil {
		t.Fatal("negative compact was accepted")
	}
}

func TestMerkleAndTxRoundTrip(t *testing.T) {
	a := SHA3([]byte("a"))
	b := SHA3([]byte("b"))
	if MerkleRoot([]Hash{a}) != a {
		t.Fatal("single-node merkle")
	}
	var buf [64]byte
	copy(buf[:32], a[:])
	copy(buf[32:], b[:])
	if MerkleRoot([]Hash{a, b}) != SHA3(buf[:]) {
		t.Fatal("pair merkle")
	}
	p := ByNetwork(Devnet)
	raw := p.Genesis().Bytes()
	block, err := DecodeBlock(raw)
	if err != nil {
		t.Fatal(err)
	}
	if block.Hash() != p.GenesisHash() {
		t.Fatal("genesis round trip")
	}
	if len(block.Txs) != 1 || len(block.Txs[0].Outputs) != 0 {
		t.Fatal("genesis is spendable")
	}
	txRaw := block.Txs[0].Bytes()
	tx, err := DecodeTx(txRaw)
	if err != nil {
		t.Fatal(err)
	}
	if tx.ID() != block.Txs[0].ID() {
		t.Fatal("txid round trip")
	}
	if _, err := DecodeTx(append(txRaw, 0)); err == nil {
		t.Fatal("trailing tx bytes accepted")
	}
}

func TestSighashBindsGenesis(t *testing.T) {
	var prev Hash
	prev[1] = 9
	tx := &Tx{
		Version: TxVersion,
		Inputs: []TxIn{{
			Prev:     Outpoint{Hash: prev, Index: 1},
			Sequence: SequenceMust,
		}},
		Outputs:  []TxOut{{Value: 10, PKH: SHA3([]byte("to"))}},
		LockTime: LockTimeMust,
	}
	var g1, g2 Hash
	g2[0] = 1
	h1 := Sighash(tx, 0, 50, SHA3([]byte("pkh")), g1)
	h2 := Sighash(tx, 0, 50, SHA3([]byte("pkh")), g2)
	if h1 == h2 {
		t.Fatal("sighash ignores genesis")
	}
	tx.Outputs[0].Value = 11
	if Sighash(tx, 0, 50, SHA3([]byte("pkh")), g1) == h1 {
		t.Fatal("sighash ignores the output")
	}
}
