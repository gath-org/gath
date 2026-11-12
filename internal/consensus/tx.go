package consensus

import (
	"bytes"
	"errors"
)

// Outpoint identifies one transaction output.
type Outpoint struct {
	Hash  Hash
	Index uint32
}

func (o Outpoint) IsCoinbase() bool {
	return o.Index == CoinbaseIndex && o.Hash.IsZero()
}

// TxOut is a payment of Value atoms to the SHA3-256 of an ML-DSA-44 public key.
type TxOut struct {
	Value uint64
	PKH   Hash
}

// TxIn spends one previous output. Script is the coinbase script, or empty
// for a spend. PubKey and Signature are the witness and are not covered by the
// transaction ID.
type TxIn struct {
	Prev      Outpoint
	Sequence  uint32
	Script    []byte
	PubKey    []byte
	Signature []byte
}

// Tx is a version-1 Gath transaction.
type Tx struct {
	Version  uint32
	Inputs   []TxIn
	Outputs  []TxOut
	LockTime uint32

	id    Hash
	idSet bool
}

// IsCoinbase reports whether this is a coinbase transaction.
// The caller has not necessarily validated it.
func (tx *Tx) IsCoinbase() bool {
	return len(tx.Inputs) == 1 && tx.Inputs[0].Prev.IsCoinbase()
}

// ID is the transaction ID: SHA3-256 of the witness-stripped serialization.
// Signatures are not covered, so a signature cannot change the ID.
func (tx *Tx) ID() Hash {
	if !tx.idSet {
		tx.id = SHA3(tx.stripped())
		tx.idSet = true
	}
	return tx.id
}

func (tx *Tx) stripped() []byte {
	w := W{}
	tx.writeBody(&w, false)
	return w.Bytes()
}

// Bytes is the full serialization, including witness.
func (tx *Tx) Bytes() []byte {
	w := W{}
	tx.writeBody(&w, true)
	return w.Bytes()
}

func (tx *Tx) writeBody(w *W, witness bool) {
	w.U32(tx.Version)
	w.VarInt(uint64(len(tx.Inputs)))
	for i := range tx.Inputs {
		in := &tx.Inputs[i]
		w.Hash(in.Prev.Hash)
		w.U32(in.Prev.Index)
		w.VarBytes(in.Script)
		w.U32(in.Sequence)
		if witness {
			w.VarBytes(in.PubKey)
			w.VarBytes(in.Signature)
		}
	}
	w.VarInt(uint64(len(tx.Outputs)))
	for i := range tx.Outputs {
		out := &tx.Outputs[i]
		w.U64(out.Value)
		w.Hash(out.PKH)
	}
	w.U32(tx.LockTime)
}

// DecodeTx parses a full transaction. Trailing bytes are rejected.
func DecodeTx(b []byte) (*Tx, error) {
	r := NewReader(b)
	tx, err := decodeTx(r)
	if err != nil {
		return nil, err
	}
	if err := r.Empty(); err != nil {
		return nil, err
	}
	return tx, nil
}

func decodeTx(r *R) (*Tx, error) {
	tx := &Tx{}
	var err error
	if tx.Version, err = r.U32(); err != nil {
		return nil, err
	}
	nIn, err := r.VarInt()
	if err != nil {
		return nil, err
	}
	if nIn > MaxInputs {
		return nil, errors.New("too many inputs")
	}
	tx.Inputs = make([]TxIn, nIn)
	for i := range tx.Inputs {
		in := &tx.Inputs[i]
		if in.Prev.Hash, err = r.Hash(); err != nil {
			return nil, err
		}
		if in.Prev.Index, err = r.U32(); err != nil {
			return nil, err
		}
		if in.Script, err = r.VarBytes(MaxCoinbaseScript); err != nil {
			return nil, err
		}
		if in.Sequence, err = r.U32(); err != nil {
			return nil, err
		}
		// Public keys and signatures are bounded by the ML-DSA-44 sizes,
		// with headroom so a wrong length fails the consensus check rather
		// than the decoder. 8 KiB is above both (1312 and 2420).
		if in.PubKey, err = r.VarBytes(8192); err != nil {
			return nil, err
		}
		if in.Signature, err = r.VarBytes(8192); err != nil {
			return nil, err
		}
	}
	nOut, err := r.VarInt()
	if err != nil {
		return nil, err
	}
	if nOut > MaxOutputs {
		return nil, errors.New("too many outputs")
	}
	tx.Outputs = make([]TxOut, nOut)
	for i := range tx.Outputs {
		if tx.Outputs[i].Value, err = r.U64(); err != nil {
			return nil, err
		}
		if tx.Outputs[i].PKH, err = r.Hash(); err != nil {
			return nil, err
		}
	}
	if tx.LockTime, err = r.U32(); err != nil {
		return nil, err
	}
	return tx, nil
}

// Sighash is the 32-byte message signed by an input.
// It commits to the whole transaction (every input outpoint and every output),
// to the value and key hash of the output being spent, and to the genesis
// hash so a transaction cannot be replayed on another network.
func Sighash(tx *Tx, input int, prevValue uint64, prevPKH Hash, genesis Hash) Hash {
	w := W{}
	w.Hash(genesis)
	w.U32(tx.Version)
	w.U32(tx.LockTime)

	prevouts := W{}
	seqs := W{}
	for i := range tx.Inputs {
		prevouts.Hash(tx.Inputs[i].Prev.Hash)
		prevouts.U32(tx.Inputs[i].Prev.Index)
		seqs.U32(tx.Inputs[i].Sequence)
	}
	outs := W{}
	for i := range tx.Outputs {
		outs.U64(tx.Outputs[i].Value)
		outs.Hash(tx.Outputs[i].PKH)
	}
	w.Hash(SHA3(prevouts.Bytes()))
	w.Hash(SHA3(seqs.Bytes()))
	w.Hash(SHA3(outs.Bytes()))
	w.U32(uint32(input))
	w.U64(prevValue)
	w.Hash(prevPKH)
	w.U32(tx.Inputs[input].Sequence)
	return SHA3(w.Bytes())
}

// CoinbaseScript is height (uint32 LE) || extraNonce (uint64 LE) || tag.
func CoinbaseScript(height uint64, extra uint64, tag []byte) ([]byte, error) {
	if height > 0xffffffff {
		return nil, errors.New("height does not fit in the coinbase script")
	}
	if 4+8+len(tag) > MaxCoinbaseScript {
		return nil, errors.New("coinbase script too long")
	}
	w := W{}
	w.U32(uint32(height))
	w.U64(extra)
	w.Raw(tag)
	return w.Bytes(), nil
}

// NewCoinbase builds a coinbase transaction paying value atoms to payTo.
// A zero value produces a coinbase with no outputs (the subsidy is not claimed).
func NewCoinbase(height, value uint64, payTo Hash, extra uint64, tag []byte) (*Tx, error) {
	script, err := CoinbaseScript(height, extra, tag)
	if err != nil {
		return nil, err
	}
	tx := &Tx{
		Version: TxVersion,
		Inputs: []TxIn{{
			Prev:     Outpoint{Index: CoinbaseIndex},
			Sequence: SequenceMust,
			Script:   script,
		}},
		LockTime: LockTimeMust,
	}
	if value > 0 {
		tx.Outputs = []TxOut{{Value: value, PKH: payTo}}
	}
	return tx, nil
}

// EqualTx reports whether two transactions have the same full serialization.
func EqualTx(a, b *Tx) bool {
	return bytes.Equal(a.Bytes(), b.Bytes())
}
