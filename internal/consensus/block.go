package consensus

import "errors"

// BlockHeader is the proof-of-work header. The nonce is last so a miner can
// overwrite the final 8 bytes without touching the rest.
type BlockHeader struct {
	Version uint32
	Prev    Hash
	Merkle  Hash
	Time    uint64
	Bits    uint32
	Nonce   uint64
}

// Bytes serializes the header. The encoding is 88 bytes.
func (h BlockHeader) Bytes() []byte {
	w := W{}
	w.U32(h.Version)
	w.Hash(h.Prev)
	w.Hash(h.Merkle)
	w.U64(h.Time)
	w.U32(h.Bits)
	w.U64(h.Nonce)
	return w.Bytes()
}

// Hash is the proof-of-work hash, SHA3-256 of the header.
func (h BlockHeader) Hash() Hash {
	return SHA3(h.Bytes())
}

// HeaderLen is the serialized header size.
const HeaderLen = 4 + 32 + 32 + 8 + 4 + 8

// Block is a header plus its transactions. The coinbase is Txs[0].
type Block struct {
	Header BlockHeader
	Txs    []*Tx
}

// Hash is the header hash.
func (b *Block) Hash() Hash { return b.Header.Hash() }

// Bytes serializes the block.
func (b *Block) Bytes() []byte {
	w := W{}
	w.Raw(b.Header.Bytes())
	w.VarInt(uint64(len(b.Txs)))
	for _, tx := range b.Txs {
		w.Raw(tx.Bytes())
	}
	return w.Bytes()
}

// DecodeBlock parses a block. The buffer must contain exactly one block.
func DecodeBlock(b []byte) (*Block, error) {
	if len(b) > MaxBlockBytes {
		return nil, rule("bad-block", "block too large")
	}
	r := NewReader(b)
	block, err := decodeBlock(r)
	if err != nil {
		return nil, err
	}
	if err := r.Empty(); err != nil {
		return nil, err
	}
	return block, nil
}

func decodeBlock(r *R) (*Block, error) {
	if r.Remaining() < HeaderLen {
		return nil, errShort
	}
	var h BlockHeader
	var err error
	if h.Version, err = r.U32(); err != nil {
		return nil, err
	}
	if h.Prev, err = r.Hash(); err != nil {
		return nil, err
	}
	if h.Merkle, err = r.Hash(); err != nil {
		return nil, err
	}
	if h.Time, err = r.U64(); err != nil {
		return nil, err
	}
	if h.Bits, err = r.U32(); err != nil {
		return nil, err
	}
	if h.Nonce, err = r.U64(); err != nil {
		return nil, err
	}
	n, err := r.VarInt()
	if err != nil {
		return nil, err
	}
	if n == 0 || n > MaxBlockTxs {
		return nil, errors.New("bad transaction count")
	}
	txs := make([]*Tx, n)
	for i := uint64(0); i < n; i++ {
		tx, err := decodeTx(r)
		if err != nil {
			return nil, err
		}
		txs[i] = tx
	}
	return &Block{Header: h, Txs: txs}, nil
}

// UTXO is one unspent output plus the context needed to spend it.
type UTXO struct {
	Value    uint64
	PKH      Hash
	Height   uint64
	Coinbase bool
}

// State is the view a block is applied to. ConnectBlock mutates it.
// HasTx reports whether a transaction ID is already in the active chain
// (after any disconnects the caller has simulated).
type State interface {
	GetUTXO(op Outpoint) (UTXO, bool)
	PutUTXO(op Outpoint, u UTXO)
	DelUTXO(op Outpoint)
	HasTx(id Hash) bool
	AddTx(id Hash)
	DelTx(id Hash)
}

// Undo records the outputs spent by a block, in transaction and input order,
// so the block can be disconnected. Coinbase spends nothing; its entry is nil.
type Undo struct {
	Spent [][]UTXO
}

// Bytes serializes an undo record.
func (u *Undo) Bytes() []byte {
	w := W{}
	w.VarInt(uint64(len(u.Spent)))
	for _, tx := range u.Spent {
		w.VarInt(uint64(len(tx)))
		for i := range tx {
			putUTXO(&w, &tx[i])
		}
	}
	return w.Bytes()
}

// DecodeUndo parses an undo record.
func DecodeUndo(b []byte) (*Undo, error) {
	r := NewReader(b)
	n, err := r.VarInt()
	if err != nil {
		return nil, err
	}
	if n > MaxBlockTxs {
		return nil, errors.New("undo too large")
	}
	u := &Undo{Spent: make([][]UTXO, n)}
	for i := uint64(0); i < n; i++ {
		m, err := r.VarInt()
		if err != nil {
			return nil, err
		}
		if m > MaxInputs {
			return nil, errors.New("undo too large")
		}
		if m == 0 {
			continue
		}
		u.Spent[i] = make([]UTXO, m)
		for j := uint64(0); j < m; j++ {
			utxo, err := readUTXO(r)
			if err != nil {
				return nil, err
			}
			u.Spent[i][j] = utxo
		}
	}
	if err := r.Empty(); err != nil {
		return nil, err
	}
	return u, nil
}

func putUTXO(w *W, u *UTXO) {
	w.U64(u.Value)
	w.Hash(u.PKH)
	w.U64(u.Height)
	if u.Coinbase {
		w.U8(1)
	} else {
		w.U8(0)
	}
}

func readUTXO(r *R) (UTXO, error) {
	var u UTXO
	var err error
	if u.Value, err = r.U64(); err != nil {
		return u, err
	}
	if u.PKH, err = r.Hash(); err != nil {
		return u, err
	}
	if u.Height, err = r.U64(); err != nil {
		return u, err
	}
	flag, err := r.U8()
	if err != nil {
		return u, err
	}
	switch flag {
	case 0:
		u.Coinbase = false
	case 1:
		u.Coinbase = true
	default:
		return u, errors.New("bad coinbase flag")
	}
	return u, nil
}
