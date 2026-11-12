package miner

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/gath-org/gath/internal/consensus"
)

func TestSolveWatchFindsNonceAndCounts(t *testing.T) {
	block := &consensus.Block{Header: consensus.BlockHeader{
		Version: consensus.BlockVersion,
		Bits:    consensus.DevnetPowBits,
		Time:    1,
	}}
	var hashes atomic.Uint64
	if err := SolveWatch(context.Background(), block, 2, &hashes); err != nil {
		t.Fatal(err)
	}
	if hashes.Load() == 0 {
		t.Fatal("hash counter stayed at zero")
	}
	target, err := consensus.CompactToTarget(block.Header.Bits)
	if err != nil {
		t.Fatal(err)
	}
	if !consensus.HashMeetsTarget(block.Hash(), target) {
		t.Fatal("solved header does not meet the target")
	}
}
