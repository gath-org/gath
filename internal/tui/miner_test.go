package tui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gath-org/gath/internal/consensus"
)

type scriptChain struct {
	mu        sync.Mutex
	submitted []*consensus.Block
}

func (s *scriptChain) Template(context.Context, string) (*consensus.Block, error) {
	return &consensus.Block{Header: consensus.BlockHeader{
		Version: consensus.BlockVersion,
		Bits:    consensus.DevnetPowBits,
		Time:    uint64(time.Now().Unix()),
	}}, nil
}

func (s *scriptChain) SubmitBlock(_ context.Context, block *consensus.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.submitted = append(s.submitted, block)
	return nil
}

func (s *scriptChain) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.submitted)
}

func TestMinerStartStopAndThreads(t *testing.T) {
	m := NewMiner(1)
	if err := m.Start(nil, "gathd1"); err == nil {
		t.Fatal("missing node should fail")
	}
	chain := &scriptChain{}
	if err := m.Start(chain, ""); err == nil {
		t.Fatal("missing address should fail")
	}
	if err := m.Start(chain, "gathd1qqqq"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for m.Status().Blocks < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	st := m.Status()
	if !st.Running || st.Blocks < 1 || st.Hashes == 0 {
		t.Fatalf("running=%v blocks=%d hashes=%d err=%s", st.Running, st.Blocks, st.Hashes, st.LastError)
	}
	if chain.count() < 1 {
		t.Fatal("no submission")
	}
	next := m.SetThreads(4)
	if next.Threads != 4 {
		t.Fatalf("threads %d", next.Threads)
	}
	if got := m.SetThreads(0).Threads; got != minThreads {
		t.Fatalf("min %d", got)
	}
	if got := m.SetThreads(maxThreads + 20).Threads; got != maxThreads {
		t.Fatalf("max %d", got)
	}
	stopped := m.Stop()
	if stopped.Running {
		t.Fatal("still running")
	}
	if stopped.Blocks < 1 {
		t.Fatal("stop cleared the block count")
	}
	found := chain.count()
	time.Sleep(50 * time.Millisecond)
	if chain.count() != found {
		t.Fatal("miner submitted after stop")
	}
}
