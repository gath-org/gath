package address

import (
	"testing"

	"github.com/gath-org/gath/internal/consensus"
)

func TestRoundTrip(t *testing.T) {
	var pkh consensus.Hash
	pkh[0] = 0xff
	pkh[31] = 0x10
	for _, hrp := range []string{"gath", "gathd"} {
		addr, err := Encode(hrp, pkh)
		if err != nil {
			t.Fatal(err)
		}
		gotHRP, got, err := Decode(addr)
		if err != nil {
			t.Fatal(err)
		}
		if gotHRP != hrp || got != pkh {
			t.Fatalf("got %s %x", gotHRP, got)
		}
		upper := []byte(addr)
		if len(upper) > 0 {
			// Mixed case is rejected by bech32.
			upper[len(upper)-1] ^= 0x20
		}
	}
	if _, _, err := Decode("gath1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq8s"); err == nil {
		t.Fatal("accepted a garbage address")
	}
}
