package consensus

import (
	"testing"
)

func TestGenesisPow(t *testing.T) {
	for _, n := range []Network{Mainnet, Devnet} {
		p := ByNetwork(n)
		g := p.Genesis()
		if len(g.Txs) != 1 || len(g.Txs[0].Outputs) != 0 {
			t.Fatalf("%s genesis has spendable outputs", p.Name)
		}
		target, err := CompactToTarget(g.Header.Bits)
		if err != nil {
			t.Fatal(err)
		}
		if !HashMeetsTarget(g.Hash(), target) {
			t.Fatalf("%s genesis nonce %d does not meet the target; hash %s", p.Name, g.Header.Nonce, g.Hash())
		}
		if g.Header.Bits != p.PowLimitBits {
			t.Fatalf("%s genesis bits", p.Name)
		}
	}
}

func TestGenesisNetworksDiffer(t *testing.T) {
	if ByNetwork(Mainnet).GenesisHash() == ByNetwork(Devnet).GenesisHash() {
		t.Fatal("mainnet and devnet share a genesis hash")
	}
}
