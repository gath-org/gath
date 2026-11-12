package mempool

import (
	"errors"
	"math/bits"
	"sync"
	"time"

	"github.com/gath-org/gath/internal/chain"
	"github.com/gath-org/gath/internal/consensus"
)

// ErrKnown means the transaction is already in the mempool or the chain.
var ErrKnown = errors.New("transaction already known")

const (
	maxBytes = 32 << 20
	maxTxs   = 5000
	expiry   = 14 * 24 * time.Hour
)

// Mempool holds valid transactions waiting for a block.
// Conflicting spends are rejected. The first transaction seen keeps the input.
type Mempool struct {
	mu       sync.Mutex
	params   *consensus.Params
	chain    *chain.Chain
	txs      map[consensus.Hash]*entry
	spent    map[consensus.Outpoint]consensus.Hash
	utxo     map[consensus.Outpoint]consensus.UTXO
	parentOf map[consensus.Outpoint]consensus.Hash
	children map[consensus.Hash][]consensus.Hash
	bytes    int
}

type entry struct {
	tx    *consensus.Tx
	fee   uint64
	size  int
	added time.Time
}

// New returns an empty mempool for a chain.
func New(params *consensus.Params, ch *chain.Chain) *Mempool {
	return &Mempool{
		params:   params,
		chain:    ch,
		txs:      make(map[consensus.Hash]*entry),
		spent:    make(map[consensus.Outpoint]consensus.Hash),
		utxo:     make(map[consensus.Outpoint]consensus.UTXO),
		parentOf: make(map[consensus.Outpoint]consensus.Hash),
		children: make(map[consensus.Hash][]consensus.Hash),
	}
}

// Len is the number of transactions in the pool.
func (m *Mempool) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.txs)
}

// Get returns a transaction and true if it is in the pool.
func (m *Mempool) Get(id consensus.Hash) (*consensus.Tx, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.txs[id]
	if !ok {
		return nil, false
	}
	return e.tx, true
}

// Spends reports whether a mempool transaction already spends op.
func (m *Mempool) Spends(op consensus.Outpoint) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.spent[op]
	return ok
}

// IDs returns the transaction IDs currently in the pool.
func (m *Mempool) IDs() []consensus.Hash {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]consensus.Hash, 0, len(m.txs))
	for id := range m.txs {
		out = append(out, id)
	}
	return out
}

// Add validates tx against the chain and the pool and stores it.
func (m *Mempool) Add(tx *consensus.Tx) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	id := tx.ID()
	if _, ok := m.txs[id]; ok {
		return ErrKnown
	}
	has, err := m.chain.HasTx(id)
	if err != nil {
		return err
	}
	if has {
		return ErrKnown
	}
	for i := range tx.Inputs {
		if _, ok := m.spent[tx.Inputs[i].Prev]; ok {
			return &consensus.RuleError{Code: "conflict", Msg: "input is already spent in the mempool"}
		}
	}
	if len(m.txs) >= maxTxs || m.bytes+len(tx.Bytes()) > maxBytes {
		return &consensus.RuleError{Code: "mempool-full", Msg: "mempool is full"}
	}
	height := m.chain.TipHeight() + 1
	view := &overlay{m: m}
	fee, err := consensus.ApplyPayment(m.params, tx, height, view)
	if err != nil {
		return err
	}
	if view.err != nil {
		return view.err
	}
	e := &entry{tx: tx, fee: fee, size: len(tx.Bytes()), added: time.Now()}
	m.txs[id] = e
	m.bytes += e.size
	for i := range tx.Inputs {
		op := tx.Inputs[i].Prev
		m.spent[op] = id
		if parent, ok := m.parentOf[op]; ok {
			m.children[parent] = append(m.children[parent], id)
		}
	}
	for i := range tx.Outputs {
		op := consensus.Outpoint{Hash: id, Index: uint32(i)}
		m.utxo[op] = consensus.UTXO{
			Value:  tx.Outputs[i].Value,
			PKH:    tx.Outputs[i].PKH,
			Height: height,
		}
		m.parentOf[op] = id
	}
	return nil
}

// Select returns transactions for a block template, highest fee rate first,
// with parents ordered before children. maxBytes is the budget for these
// transactions, not including the coinbase.
func (m *Mempool) Select(maxBytes int) ([]*consensus.Tx, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if maxBytes <= 0 {
		return nil, 0
	}
	items := make([]*entry, 0, len(m.txs))
	for _, e := range m.txs {
		items = append(items, e)
	}
	selected := make(map[consensus.Hash]bool, len(items))
	var (
		out  []*consensus.Tx
		fees uint64
		used int
	)
	for {
		var best *entry
		for _, e := range items {
			id := e.tx.ID()
			if selected[id] || used+e.size > maxBytes || !m.ready(e, selected) {
				continue
			}
			if best == nil || better(e, best) {
				best = e
			}
		}
		if best == nil {
			break
		}
		selected[best.tx.ID()] = true
		out = append(out, best.tx)
		fees += best.fee
		used += best.size
	}
	return out, fees
}

func (m *Mempool) ready(e *entry, selected map[consensus.Hash]bool) bool {
	for i := range e.tx.Inputs {
		parent, ok := m.parentOf[e.tx.Inputs[i].Prev]
		if ok && !selected[parent] {
			return false
		}
	}
	return true
}

func better(a, b *entry) bool {
	hi1, lo1 := bits.Mul64(a.fee, uint64(b.size))
	hi2, lo2 := bits.Mul64(b.fee, uint64(a.size))
	if hi1 != hi2 {
		return hi1 > hi2
	}
	if lo1 != lo2 {
		return lo1 > lo2
	}
	return a.tx.ID().String() < b.tx.ID().String()
}

// OnReorg clears the pool and returns transactions the caller should try to
// add again: non-coinbase transactions from disconnected blocks, oldest
// first, then the previous pool contents with parents before children.
// Confirmed transactions and transactions whose inputs are gone fail Add and
// stay out.
func (m *Mempool) OnReorg(acc chain.Accept) []*consensus.Tx {
	if !acc.TipChanged {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var back []*consensus.Tx
	for i := len(acc.Removed) - 1; i >= 0; i-- {
		for _, tx := range acc.Removed[i].Txs {
			if tx.IsCoinbase() {
				continue
			}
			back = append(back, tx)
		}
	}
	back = append(back, m.ordered()...)
	m.txs = make(map[consensus.Hash]*entry)
	m.spent = make(map[consensus.Outpoint]consensus.Hash)
	m.utxo = make(map[consensus.Outpoint]consensus.UTXO)
	m.parentOf = make(map[consensus.Outpoint]consensus.Hash)
	m.children = make(map[consensus.Hash][]consensus.Hash)
	m.bytes = 0
	return back
}

func (m *Mempool) ordered() []*consensus.Tx {
	left := make(map[consensus.Hash]*entry, len(m.txs))
	for id, e := range m.txs {
		left[id] = e
	}
	var out []*consensus.Tx
	for len(left) > 0 {
		progress := false
		for id, e := range left {
			ready := true
			for i := range e.tx.Inputs {
				parent, ok := m.parentOf[e.tx.Inputs[i].Prev]
				if ok && left[parent] != nil {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			out = append(out, e.tx)
			delete(left, id)
			progress = true
		}
		if !progress {
			for _, e := range left {
				out = append(out, e.tx)
			}
			break
		}
	}
	return out
}

func (m *Mempool) remove(id consensus.Hash) {
	kids := append([]consensus.Hash(nil), m.children[id]...)
	for _, k := range kids {
		m.remove(k)
	}
	e, ok := m.txs[id]
	if !ok {
		delete(m.children, id)
		return
	}
	delete(m.txs, id)
	m.bytes -= e.size
	for i := range e.tx.Inputs {
		op := e.tx.Inputs[i].Prev
		if m.spent[op] == id {
			delete(m.spent, op)
		}
	}
	for i := range e.tx.Outputs {
		op := consensus.Outpoint{Hash: id, Index: uint32(i)}
		delete(m.utxo, op)
		delete(m.parentOf, op)
	}
	delete(m.children, id)
}

func (m *Mempool) expire() {
	cutoff := time.Now().Add(-expiry)
	var old []consensus.Hash
	for id, e := range m.txs {
		if e.added.Before(cutoff) {
			old = append(old, id)
		}
	}
	for _, id := range old {
		m.remove(id)
	}
}

// overlay is a consensus.State over the chain plus mempool outputs.
// Mutations stay in the overlay; Add copies them into the pool on success.
type overlay struct {
	m   *Mempool
	put map[consensus.Outpoint]consensus.UTXO
	del map[consensus.Outpoint]bool
	tx  map[consensus.Hash]bool
	err error
}

func (o *overlay) GetUTXO(op consensus.Outpoint) (consensus.UTXO, bool) {
	if o.err != nil {
		return consensus.UTXO{}, false
	}
	if o.del[op] {
		return consensus.UTXO{}, false
	}
	if u, ok := o.put[op]; ok {
		return u, true
	}
	if u, ok := o.m.utxo[op]; ok {
		return u, true
	}
	u, ok, err := o.m.chain.LookupUTXO(op)
	if err != nil {
		o.err = err
		return consensus.UTXO{}, false
	}
	return u, ok
}

func (o *overlay) PutUTXO(op consensus.Outpoint, u consensus.UTXO) {
	if o.put == nil {
		o.put = make(map[consensus.Outpoint]consensus.UTXO)
	}
	o.put[op] = u
	delete(o.del, op)
}

func (o *overlay) DelUTXO(op consensus.Outpoint) {
	if o.del == nil {
		o.del = make(map[consensus.Outpoint]bool)
	}
	o.del[op] = true
	delete(o.put, op)
}

func (o *overlay) HasTx(id consensus.Hash) bool {
	if o.err != nil {
		return false
	}
	if v, ok := o.tx[id]; ok {
		return v
	}
	if _, ok := o.m.txs[id]; ok {
		return true
	}
	ok, err := o.m.chain.HasTx(id)
	if err != nil {
		o.err = err
		return false
	}
	return ok
}

func (o *overlay) AddTx(id consensus.Hash) {
	if o.tx == nil {
		o.tx = make(map[consensus.Hash]bool)
	}
	o.tx[id] = true
}

func (o *overlay) DelTx(id consensus.Hash) {
	if o.tx == nil {
		o.tx = make(map[consensus.Hash]bool)
	}
	o.tx[id] = false
}
