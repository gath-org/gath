package miner

import (
	"context"
	"encoding/binary"
	"errors"
	"math/big"
	"sync/atomic"

	"golang.org/x/crypto/sha3"

	"github.com/gath-org/gath/internal/consensus"
)

// Solve searches for a nonce that satisfies the block's compact target.
// threads is the number of CPU workers. On success the header nonce is set.
func Solve(ctx context.Context, block *consensus.Block, threads int) error {
	if block == nil {
		return errors.New("nil block")
	}
	if threads < 1 {
		threads = 1
	}
	target, err := consensus.CompactToTarget(block.Header.Bits)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var counter atomic.Uint64
	var found atomic.Uint64
	var ok atomic.Bool
	errc := make(chan error, threads)
	for i := 0; i < threads; i++ {
		go func() {
			errc <- search(ctx, block.Header, target, &counter, &found, &ok)
		}()
	}
	var first error
	for i := 0; i < threads; i++ {
		err := <-errc
		if err != nil && first == nil && !ok.Load() {
			first = err
		}
	}
	if ok.Load() {
		block.Header.Nonce = found.Load()
		return nil
	}
	if first == nil {
		first = errors.New("no nonce found")
	}
	return first
}

func search(ctx context.Context, header consensus.BlockHeader, target *big.Int, counter, found *atomic.Uint64, ok *atomic.Bool) error {
	buf := header.Bytes()
	if len(buf) != consensus.HeaderLen {
		return errors.New("bad header length")
	}
	for {
		if ok.Load() {
			return nil
		}
		n := counter.Add(1) - 1
		if n&0xfff == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		binary.LittleEndian.PutUint64(buf[consensus.HeaderLen-8:], n)
		sum := sha3.Sum256(buf)
		var h consensus.Hash
		copy(h[:], sum[:])
		if consensus.HashMeetsTarget(h, target) {
			if ok.CompareAndSwap(false, true) {
				found.Store(n)
			}
			return nil
		}
		if n == ^uint64(0) {
			return errors.New("nonce space exhausted")
		}
	}
}
