package tui

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/miner"
)

const (
	minThreads = 1
	maxThreads = 256
)

var (
	errNoNode   = errors.New("node is not running")
	errNoWallet = errors.New("unlock the wallet for this network first")
)

// MinerStatus is the miner as the terminal shows it.
type MinerStatus struct {
	Running   bool
	Threads   int
	Hashrate  float64
	Measuring bool
	Hashes    uint64
	Blocks    uint64
	LastHash  string
	LastError string
	Address   string
}

type chainAPI interface {
	Template(ctx context.Context, address string) (*consensus.Block, error)
	SubmitBlock(ctx context.Context, block *consensus.Block) error
}

// Miner fetches templates, hashes them, and submits blocks until stopped.
type Miner struct {
	mu          sync.Mutex
	threads     int
	running     bool
	address     string
	blocks      uint64
	lastHash    string
	lastErr     string
	cancel      context.CancelFunc
	solveCancel context.CancelFunc
	done        chan struct{}
	hashes      atomic.Uint64
	rate        float64
	rateAt      time.Time
	rateHashes  uint64
}

// NewMiner returns a stopped miner with threads workers.
func NewMiner(threads int) *Miner {
	if threads < minThreads {
		threads = minThreads
	}
	if threads > maxThreads {
		threads = maxThreads
	}
	return &Miner{threads: threads}
}

// Start begins hashing for address. A miner that is already running is stopped first.
func (m *Miner) Start(api chainAPI, address string) error {
	if api == nil {
		return errNoNode
	}
	if address == "" {
		return errNoWallet
	}
	m.halt(false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.mu.Lock()
	m.address = address
	m.running = true
	m.cancel = cancel
	m.done = done
	m.lastErr = ""
	m.rateAt = time.Now()
	m.rateHashes = m.hashes.Load()
	m.rate = 0
	m.mu.Unlock()
	go m.loop(ctx, done, api, address)
	return nil
}

// Stop ends hashing and keeps the block count from this run.
func (m *Miner) Stop() MinerStatus {
	m.halt(false)
	return m.Status()
}

// Reset ends hashing and clears the counters. A network switch uses this.
func (m *Miner) Reset() MinerStatus {
	m.halt(true)
	return m.Status()
}

// SetThreads changes the worker count. The new count applies on the next template,
// and an in-progress search is cancelled so it does not wait for a block.
func (m *Miner) SetThreads(n int) MinerStatus {
	if n < minThreads {
		n = minThreads
	}
	if n > maxThreads {
		n = maxThreads
	}
	m.mu.Lock()
	changed := n != m.threads
	m.threads = n
	cancel := m.solveCancel
	m.mu.Unlock()
	if changed && cancel != nil {
		cancel()
	}
	return m.Status()
}

// Status is a snapshot safe to render.
func (m *Miner) Status() MinerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	hashes := m.hashes.Load()
	elapsed := now.Sub(m.rateAt).Seconds()
	measuring := m.running && m.rate == 0 && (m.rateAt.IsZero() || elapsed < 0.5)
	if m.running && elapsed >= 0.5 {
		delta := hashes - m.rateHashes
		m.rate = float64(delta) / elapsed
		m.rateAt = now
		m.rateHashes = hashes
		measuring = false
	}
	rate := m.rate
	if !m.running {
		rate = 0
		measuring = false
	}
	return MinerStatus{
		Running:   m.running,
		Threads:   m.threads,
		Hashrate:  rate,
		Measuring: measuring,
		Hashes:    hashes,
		Blocks:    m.blocks,
		LastHash:  m.lastHash,
		LastError: m.lastErr,
		Address:   m.address,
	}
}

func (m *Miner) halt(reset bool) {
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.cancel = nil
	m.done = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	m.mu.Lock()
	m.running = false
	m.rate = 0
	m.solveCancel = nil
	if reset {
		m.blocks = 0
		m.lastHash = ""
		m.lastErr = ""
		m.address = ""
		m.hashes.Store(0)
		m.rateHashes = 0
		m.rateAt = time.Time{}
	}
	m.mu.Unlock()
}

func (m *Miner) loop(ctx context.Context, done chan struct{}, api chainAPI, address string) {
	defer close(done)
	defer func() {
		m.mu.Lock()
		m.running = false
		m.cancel = nil
		m.solveCancel = nil
		m.mu.Unlock()
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		block, err := api.Template(ctx, address)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.setErr(err)
			if !sleepCtx(ctx, time.Second) {
				return
			}
			continue
		}
		threads := m.threadCount()
		solveCtx, solveCancel := context.WithCancel(ctx)
		m.mu.Lock()
		m.solveCancel = solveCancel
		m.mu.Unlock()
		err = miner.SolveWatch(solveCtx, block, threads, &m.hashes)
		solveCancel()
		m.mu.Lock()
		if m.solveCancel != nil {
			m.solveCancel = nil
		}
		m.mu.Unlock()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, context.Canceled) {
				continue
			}
			m.setErr(err)
			if !sleepCtx(ctx, time.Second) {
				return
			}
			continue
		}
		if err := api.SubmitBlock(ctx, block); err != nil {
			if ctx.Err() != nil {
				return
			}
			m.setErr(err)
			if !sleepCtx(ctx, time.Second) {
				return
			}
			continue
		}
		m.noteBlock(block.Hash().String())
	}
}

func (m *Miner) threadCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.threads
	if n < minThreads {
		n = minThreads
	}
	return n
}

func (m *Miner) setErr(err error) {
	m.mu.Lock()
	m.lastErr = err.Error()
	m.mu.Unlock()
}

func (m *Miner) noteBlock(hash string) {
	m.mu.Lock()
	m.blocks++
	m.lastHash = hash
	m.lastErr = ""
	m.mu.Unlock()
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
