package wallet

import (
	"testing"

	"github.com/tyler-smith/go-bip39"

	"github.com/gath-org/gath/internal/consensus"
)

func TestCreateRestoreAndPay(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/wallet.json"
	phrase, w, err := Create(path, "devnet", "devnet-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if !bip39.IsMnemonicValid(phrase) {
		t.Fatal("phrase is not BIP39")
	}
	words := 0
	for _, r := range phrase {
		if r == ' ' {
			words++
		}
	}
	if words != 23 {
		t.Fatalf("phrase has %d spaces", words)
	}
	loaded, err := Load(path, "devnet-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PKH() != w.PKH() {
		t.Fatal("loaded key differs")
	}
	if _, err := Load(path, "wrong-passphrase"); err == nil {
		t.Fatal("wrong passphrase unlocked the file")
	}
	restored, err := Restore(dir+"/restored.json", "devnet", phrase, "another-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	addr, err := w.Address()
	if err != nil {
		t.Fatal(err)
	}
	addr2, err := restored.Address()
	if err != nil {
		t.Fatal(err)
	}
	if addr != addr2 {
		t.Fatalf("restore address %s, created %s", addr2, addr)
	}

	entropy, err := bip39.NewEntropy(128)
	if err != nil {
		t.Fatal(err)
	}
	short, err := bip39.NewMnemonic(entropy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(dir+"/short.json", "devnet", short, "devnet-passphrase"); err != nil {
		t.Fatal(err)
	}

	var payTo consensus.Hash
	payTo[0] = 7
	var prev consensus.Hash
	prev[1] = 4
	const value = 500_000_000
	tx, err := w.BuildPayment(payTo, 100_000_000, 1, 100, []Coin{{
		Outpoint: consensus.Outpoint{Hash: prev, Index: 0},
		Value:    value,
		Height:   1,
		Coinbase: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	st := consensus.NewMapState()
	st.PutUTXO(consensus.Outpoint{Hash: prev, Index: 0}, consensus.UTXO{
		Value:    value,
		PKH:      w.PKH(),
		Height:   1,
		Coinbase: true,
	})
	if _, err := consensus.ApplyPayment(consensus.ByNetwork(consensus.Devnet), tx, 101, st); err != nil {
		t.Fatal(err)
	}
	if _, err := w.BuildPayment(payTo, value, 1, 100, []Coin{{
		Outpoint: consensus.Outpoint{Hash: prev, Index: 0},
		Value:    value,
		Height:   1,
		Coinbase: true,
	}}); err == nil {
		t.Fatal("spent the full coin without leaving a fee")
	}
}
