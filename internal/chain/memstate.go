package chain

import (
	bolt "go.etcd.io/bbolt"

	"github.com/gath-org/gath/internal/consensus"
)

type utxoChange struct {
	utxo    consensus.UTXO
	present bool
}

// memState is a UTXO and tx-index overlay on top of the database. Reads fall
// through to the active chain. Discard it to leave the database unchanged.
type memState struct {
	c    *Chain
	utxo map[consensus.Outpoint]utxoChange
	tx   map[consensus.Hash]bool
	err  error
}

func newMemState(c *Chain) *memState {
	return &memState{
		c:    c,
		utxo: make(map[consensus.Outpoint]utxoChange),
		tx:   make(map[consensus.Hash]bool),
	}
}

func (m *memState) GetUTXO(op consensus.Outpoint) (consensus.UTXO, bool) {
	if m.err != nil {
		return consensus.UTXO{}, false
	}
	if ch, ok := m.utxo[op]; ok {
		if !ch.present {
			return consensus.UTXO{}, false
		}
		return ch.utxo, true
	}
	var (
		u  consensus.UTXO
		ok bool
	)
	err := m.c.db.View(func(tx *bolt.Tx) error {
		var err error
		u, ok, err = readUTXOTx(tx, op)
		return err
	})
	if err != nil {
		m.err = err
		return consensus.UTXO{}, false
	}
	return u, ok
}

func (m *memState) PutUTXO(op consensus.Outpoint, u consensus.UTXO) {
	m.utxo[op] = utxoChange{utxo: u, present: true}
}

func (m *memState) DelUTXO(op consensus.Outpoint) {
	m.utxo[op] = utxoChange{present: false}
}

func (m *memState) HasTx(id consensus.Hash) bool {
	if m.err != nil {
		return false
	}
	if v, ok := m.tx[id]; ok {
		return v
	}
	var found bool
	err := m.c.db.View(func(tx *bolt.Tx) error {
		_, _, _, ok, err := readTxIndex(tx, id)
		found = ok
		return err
	})
	if err != nil {
		m.err = err
		return false
	}
	return found
}

func (m *memState) AddTx(id consensus.Hash) { m.tx[id] = true }

func (m *memState) DelTx(id consensus.Hash) { m.tx[id] = false }
