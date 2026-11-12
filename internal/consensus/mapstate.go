package consensus

// MapState is an in-memory UTXO set used by tests and by the mempool.
type MapState struct {
	U map[Outpoint]UTXO
	T map[Hash]struct{}
}

// NewMapState returns an empty state.
func NewMapState() *MapState {
	return &MapState{
		U: make(map[Outpoint]UTXO),
		T: make(map[Hash]struct{}),
	}
}

func (s *MapState) GetUTXO(op Outpoint) (UTXO, bool) {
	u, ok := s.U[op]
	return u, ok
}

func (s *MapState) PutUTXO(op Outpoint, u UTXO) { s.U[op] = u }

func (s *MapState) DelUTXO(op Outpoint) { delete(s.U, op) }

func (s *MapState) HasTx(id Hash) bool {
	_, ok := s.T[id]
	return ok
}

func (s *MapState) AddTx(id Hash) { s.T[id] = struct{}{} }

func (s *MapState) DelTx(id Hash) { delete(s.T, id) }

// Clone returns a deep copy of the UTXO map and the tx set.
func (s *MapState) Clone() *MapState {
	n := NewMapState()
	for k, v := range s.U {
		n.U[k] = v
	}
	for k := range s.T {
		n.T[k] = struct{}{}
	}
	return n
}
