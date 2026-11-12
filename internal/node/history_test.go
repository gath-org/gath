package node

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/gath-org/gath/internal/address"
	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/keys"
)

func TestAddressHistoryReceive(t *testing.T) {
	ctx := context.Background()
	n, err := Start(ctx, Config{
		Network: "devnet",
		DataDir: t.TempDir(),
		Listen:  "127.0.0.1:0",
		RPC:     "127.0.0.1:0",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })

	seed := make([]byte, 64)
	seed[0] = 9
	key, err := keys.Derive(seed, 0)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := address.Encode(n.params.HRP, key.PKH())
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Generate(ctx, key.PKH(), 1); err != nil {
		t.Fatal(err)
	}
	hist, err := n.AddressHistory(addr)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist.Transactions) != 1 {
		t.Fatalf("history %+v", hist)
	}
	got := hist.Transactions[0]
	if !got.Confirmed || got.Height != 1 || got.ReceivedAtoms != "500000000" || got.NetAtoms != "500000000" || got.SentAtoms != "0" {
		t.Fatalf("row %+v", got)
	}
	if _, err := consensus.ParseHash(got.TxID); err != nil {
		t.Fatal(err)
	}
}
