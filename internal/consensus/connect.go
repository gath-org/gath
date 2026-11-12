package consensus

import (
	"encoding/binary"
	"math"
	"math/big"
	"sort"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"
)

// PubKeyHash is the address payload: SHA3-256 of the ML-DSA-44 public key.
func PubKeyHash(pub []byte) Hash { return SHA3(pub) }

func addAtoms(a, b uint64) (uint64, error) {
	if a > math.MaxUint64-b {
		return 0, rule("overflow", "value overflow")
	}
	return a + b, nil
}

// MedianTime is the median-time-past of the given timestamps.
// Bitcoin's rule: sort ascending and take index len/2.
func MedianTime(times []uint64) uint64 {
	if len(times) == 0 {
		return 0
	}
	s := make([]uint64, len(times))
	copy(s, times)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

// AncestorTimes returns up to MedianTimeBlocks timestamps ending at parent,
// walking ParentOf. The slice is oldest-first. ParentOf returns false at genesis's parent.
func AncestorTimes(parentHash Hash, parentOf func(Hash) (header BlockHeader, height uint64, ok bool)) []uint64 {
	var rev []uint64
	h := parentHash
	for len(rev) < MedianTimeBlocks {
		hdr, _, ok := parentOf(h)
		if !ok {
			break
		}
		rev = append(rev, hdr.Time)
		if hdr.Prev.IsZero() {
			break
		}
		h = hdr.Prev
	}
	// rev is newest-first. MedianTime sorts, so order does not matter.
	return rev
}

// ConnectBlock validates block against state and applies it.
// On error, state may be partially modified and must be discarded.
// parentHeight is the height of parent. The block's height is parentHeight+1.
// grandparent is required when parentHeight > 0. ancestorTimes are the
// timestamps the median-time-past rule is computed from (the parent and up
// to ten of its ancestors). now is the node's unix time. checkFuture
// enforces the two-hour future limit.
func ConnectBlock(p *Params, block *Block, parent *BlockHeader, parentHeight uint64, grandparent *BlockHeader, ancestorTimes []uint64, now int64, checkFuture bool, state State) (*Undo, error) {
	if block == nil || parent == nil || state == nil || p == nil {
		return nil, rule("bad-block", "missing block")
	}
	height := parentHeight + 1
	if err := CheckTime(block.Header.Time, ancestorTimes, now, checkFuture); err != nil {
		return nil, err
	}
	if err := checkHeader(p, block, parent, parentHeight, grandparent); err != nil {
		return nil, err
	}
	if err := checkBlockShape(block, height); err != nil {
		return nil, err
	}
	return applyBlock(p, block, height, state)
}

func checkHeader(p *Params, block *Block, parent *BlockHeader, parentHeight uint64, grandparent *BlockHeader) error {
	h := &block.Header
	if h.Version != BlockVersion {
		return rule("bad-version", "unsupported block version")
	}
	if h.Prev != parent.Hash() {
		return rule("bad-parent", "previous hash mismatch")
	}
	if h.Time > uint64(math.MaxInt64) || parent.Time > uint64(math.MaxInt64) {
		return rule("bad-time", "timestamp out of range")
	}
	want, err := NextBits(parent, parentHeight, grandparent, p)
	if err != nil {
		return err
	}
	if h.Bits != want {
		return rulef("bad-bits", "bits %08x, want %08x", h.Bits, want)
	}
	target, err := CompactToTarget(h.Bits)
	if err != nil {
		return err
	}
	if target.Cmp(p.PowLimit()) > 0 {
		return rule("bad-bits", "target above pow limit")
	}
	if !HashMeetsTarget(block.Hash(), target) {
		return rule("bad-pow", "proof of work failed")
	}
	return nil
}

// CheckTime enforces the median-time-past rule and, when checkFuture is set,
// the two-hour future limit. times are the timestamps of the ancestor chain
// the block extends, including the parent, up to 11 of them.
func CheckTime(blockTime uint64, times []uint64, now int64, checkFuture bool) error {
	if blockTime > uint64(math.MaxInt64) {
		return rule("bad-time", "timestamp out of range")
	}
	if len(times) == 0 {
		return rule("bad-time", "no ancestor time")
	}
	mtp := MedianTime(times)
	if blockTime <= mtp {
		return rule("bad-time", "timestamp is not after median time past")
	}
	if checkFuture && int64(blockTime) > now+MaxFutureSeconds {
		return rule("bad-time", "timestamp too far in the future")
	}
	return nil
}

func checkBlockShape(block *Block, height uint64) error {
	raw := block.Bytes()
	if len(raw) > MaxBlockBytes {
		return rule("bad-block", "block too large")
	}
	if len(block.Txs) == 0 {
		return rule("bad-block", "empty block")
	}
	if !block.Txs[0].IsCoinbase() {
		return rule("bad-coinbase", "first transaction is not coinbase")
	}
	ids := make([]Hash, len(block.Txs))
	seen := make(map[Hash]struct{}, len(block.Txs))
	for i, tx := range block.Txs {
		if tx == nil {
			return rule("bad-tx", "nil transaction")
		}
		if i > 0 && tx.IsCoinbase() {
			return rule("bad-coinbase", "extra coinbase")
		}
		if err := checkTxShape(tx, i == 0, height); err != nil {
			return err
		}
		id := tx.ID()
		if _, ok := seen[id]; ok {
			return rule("bad-tx", "duplicate transaction")
		}
		seen[id] = struct{}{}
		ids[i] = id
	}
	if MerkleRoot(ids) != block.Header.Merkle {
		return rule("bad-merkle", "merkle root mismatch")
	}
	return nil
}

func checkTxShape(tx *Tx, coinbase bool, height uint64) error {
	if tx.Version != TxVersion {
		return rule("bad-version", "unsupported transaction version")
	}
	if tx.LockTime != LockTimeMust {
		return rule("bad-tx", "lock time must be zero")
	}
	if len(tx.Inputs) == 0 || len(tx.Inputs) > MaxInputs {
		return rule("bad-tx", "bad input count")
	}
	if len(tx.Outputs) > MaxOutputs {
		return rule("bad-tx", "bad output count")
	}
	if len(tx.Bytes()) > MaxTxBytes {
		return rule("bad-tx", "transaction too large")
	}
	if coinbase {
		if len(tx.Inputs) != 1 {
			return rule("bad-coinbase", "coinbase input count")
		}
		in := &tx.Inputs[0]
		if !in.Prev.IsCoinbase() {
			return rule("bad-coinbase", "coinbase outpoint")
		}
		if in.Sequence != SequenceMust {
			return rule("bad-tx", "bad sequence")
		}
		if len(in.PubKey) != 0 || len(in.Signature) != 0 {
			return rule("bad-coinbase", "coinbase witness must be empty")
		}
		if len(in.Script) < 4 || len(in.Script) > MaxCoinbaseScript {
			return rule("bad-coinbase", "bad coinbase script length")
		}
		got := uint64(binary.LittleEndian.Uint32(in.Script[:4]))
		if got != height {
			return rule("bad-coinbase", "coinbase height mismatch")
		}
	} else {
		if len(tx.Outputs) == 0 {
			return rule("bad-tx", "transaction has no outputs")
		}
		seen := make(map[Outpoint]struct{}, len(tx.Inputs))
		for i := range tx.Inputs {
			in := &tx.Inputs[i]
			if in.Prev.IsCoinbase() {
				return rule("bad-tx", "null outpoint")
			}
			if in.Sequence != SequenceMust {
				return rule("bad-tx", "bad sequence")
			}
			if len(in.Script) != 0 {
				return rule("bad-tx", "spend script must be empty")
			}
			if len(in.PubKey) != mldsa44.PublicKeySize || len(in.Signature) != mldsa44.SignatureSize {
				return rule("bad-sig", "wrong witness size")
			}
			if _, ok := seen[in.Prev]; ok {
				return rule("bad-tx", "duplicate input")
			}
			seen[in.Prev] = struct{}{}
		}
	}
	for i := range tx.Outputs {
		out := &tx.Outputs[i]
		if out.Value == 0 {
			return rule("bad-tx", "zero output")
		}
		if !coinbase && out.Value < MinOutputAtoms {
			return rule("dust", "output below the minimum")
		}
	}
	return nil
}

func applyBlock(p *Params, block *Block, height uint64, state State) (*Undo, error) {
	undo := &Undo{Spent: make([][]UTXO, len(block.Txs))}
	var fees uint64
	// Spends are applied in order so a transaction may use an output created
	// earlier in the same block. Coinbase outputs are applied last: they are
	// immature and must not be spent here. Their value is checked against the
	// fees those spends produced.
	for i := 1; i < len(block.Txs); i++ {
		tx := block.Txs[i]
		spent, fee, err := applySpend(p, tx, height, state)
		if err != nil {
			return nil, err
		}
		var errAdd error
		fees, errAdd = addAtoms(fees, fee)
		if errAdd != nil {
			return nil, errAdd
		}
		undo.Spent[i] = spent
	}
	if err := applyCoinbase(p, block.Txs[0], height, fees, state); err != nil {
		return nil, err
	}
	return undo, nil
}

func applySpend(p *Params, tx *Tx, height uint64, state State) ([]UTXO, uint64, error) {
	id := tx.ID()
	if state.HasTx(id) {
		return nil, 0, rule("bad-tx", "duplicate transaction id")
	}
	spent := make([]UTXO, len(tx.Inputs))
	var inSum uint64
	for i := range tx.Inputs {
		in := &tx.Inputs[i]
		utxo, ok := state.GetUTXO(in.Prev)
		if !ok {
			return nil, 0, rule("bad-input", "missing or spent input")
		}
		if utxo.Coinbase && (height < utxo.Height || height-utxo.Height < CoinbaseMaturity) {
			return nil, 0, rule("immature", "coinbase is not mature")
		}
		if PubKeyHash(in.PubKey) != utxo.PKH {
			return nil, 0, rule("bad-sig", "public key does not match output")
		}
		msg := Sighash(tx, i, utxo.Value, utxo.PKH, p.GenesisHash())
		if err := verifySig(in.PubKey, in.Signature, msg[:]); err != nil {
			return nil, 0, err
		}
		var err error
		inSum, err = addAtoms(inSum, utxo.Value)
		if err != nil {
			return nil, 0, err
		}
		spent[i] = utxo
		state.DelUTXO(in.Prev)
	}
	var outSum uint64
	for i := range tx.Outputs {
		var err error
		outSum, err = addAtoms(outSum, tx.Outputs[i].Value)
		if err != nil {
			return nil, 0, err
		}
	}
	if outSum > inSum {
		return nil, 0, rule("bad-fee", "outputs exceed inputs")
	}
	fee := inSum - outSum
	size := uint64(len(tx.Bytes()))
	if fee < size*MinFeePerByte {
		return nil, 0, rule("bad-fee", "fee below the minimum")
	}
	for i := range tx.Outputs {
		op := Outpoint{Hash: id, Index: uint32(i)}
		state.PutUTXO(op, UTXO{
			Value:    tx.Outputs[i].Value,
			PKH:      tx.Outputs[i].PKH,
			Height:   height,
			Coinbase: false,
		})
	}
	state.AddTx(id)
	return spent, fee, nil
}

func applyCoinbase(p *Params, tx *Tx, height, fees uint64, state State) error {
	id := tx.ID()
	if state.HasTx(id) {
		return rule("bad-tx", "duplicate transaction id")
	}
	var sum uint64
	for i := range tx.Outputs {
		var err error
		sum, err = addAtoms(sum, tx.Outputs[i].Value)
		if err != nil {
			return err
		}
	}
	allowed, err := addAtoms(Subsidy(height), fees)
	if err != nil {
		return err
	}
	if sum > allowed {
		return rule("bad-subsidy", "coinbase exceeds subsidy plus fees")
	}
	for i := range tx.Outputs {
		op := Outpoint{Hash: id, Index: uint32(i)}
		state.PutUTXO(op, UTXO{
			Value:    tx.Outputs[i].Value,
			PKH:      tx.Outputs[i].PKH,
			Height:   height,
			Coinbase: true,
		})
	}
	state.AddTx(id)
	return nil
}

func verifySig(pub, sig, msg []byte) error {
	if len(pub) != mldsa44.PublicKeySize || len(sig) != mldsa44.SignatureSize {
		return rule("bad-sig", "wrong witness size")
	}
	var pk mldsa44.PublicKey
	if err := pk.UnmarshalBinary(pub); err != nil {
		return rule("bad-sig", "public key rejected")
	}
	if !mldsa44.Verify(&pk, msg, []byte(SigContext), sig) {
		return rule("bad-sig", "signature rejected")
	}
	return nil
}

// ReplayBlock applies a block that was validated before, using the undo
// record as the spent outputs. It checks that those outputs are still the
// ones in state, then performs the same mutations as ConnectBlock.
// Signatures are not checked again.
func ReplayBlock(block *Block, undo *Undo, height uint64, state State) error {
	if block == nil || undo == nil || state == nil || len(undo.Spent) != len(block.Txs) || len(block.Txs) == 0 {
		return rule("bad-undo", "replay shape")
	}
	for i := 1; i < len(block.Txs); i++ {
		tx := block.Txs[i]
		if len(undo.Spent[i]) != len(tx.Inputs) {
			return rule("bad-undo", "spent count mismatch")
		}
		id := tx.ID()
		if state.HasTx(id) {
			return rule("bad-tx", "duplicate transaction id")
		}
		for j := range tx.Inputs {
			got, ok := state.GetUTXO(tx.Inputs[j].Prev)
			if !ok || got != undo.Spent[i][j] {
				return rule("bad-undo", "spent output mismatch")
			}
			state.DelUTXO(tx.Inputs[j].Prev)
		}
		for j := range tx.Outputs {
			state.PutUTXO(Outpoint{Hash: id, Index: uint32(j)}, UTXO{
				Value:    tx.Outputs[j].Value,
				PKH:      tx.Outputs[j].PKH,
				Height:   height,
				Coinbase: false,
			})
		}
		state.AddTx(id)
	}
	cb := block.Txs[0]
	cbID := cb.ID()
	if state.HasTx(cbID) {
		return rule("bad-tx", "duplicate transaction id")
	}
	for j := range cb.Outputs {
		state.PutUTXO(Outpoint{Hash: cbID, Index: uint32(j)}, UTXO{
			Value:    cb.Outputs[j].Value,
			PKH:      cb.Outputs[j].PKH,
			Height:   height,
			Coinbase: true,
		})
	}
	state.AddTx(cbID)
	return nil
}

// ApplyPayment validates a non-coinbase transaction and applies it to state.
// height is the height of the block that would contain it (the next block, for
// the mempool). On error, state may be partially modified.
func ApplyPayment(p *Params, tx *Tx, height uint64, state State) (uint64, error) {
	if tx == nil || p == nil || state == nil {
		return 0, rule("bad-tx", "missing transaction")
	}
	if tx.IsCoinbase() {
		return 0, rule("bad-tx", "coinbase is not accepted here")
	}
	if err := checkTxShape(tx, false, height); err != nil {
		return 0, err
	}
	_, fee, err := applySpend(p, tx, height, state)
	return fee, err
}

// UndoBlock reverses a block previously applied to state.
// The undo record must be the one ConnectBlock returned for this block.
func UndoBlock(block *Block, undo *Undo, state State) error {
	if block == nil || undo == nil || len(undo.Spent) != len(block.Txs) {
		return rule("bad-undo", "undo does not match block")
	}
	cb := block.Txs[0]
	cbID := cb.ID()
	for j := range cb.Outputs {
		op := Outpoint{Hash: cbID, Index: uint32(j)}
		if _, ok := state.GetUTXO(op); !ok {
			return rule("bad-undo", "missing coinbase output")
		}
		state.DelUTXO(op)
	}
	state.DelTx(cbID)
	for i := len(block.Txs) - 1; i >= 1; i-- {
		tx := block.Txs[i]
		id := tx.ID()
		for j := range tx.Outputs {
			op := Outpoint{Hash: id, Index: uint32(j)}
			if _, ok := state.GetUTXO(op); !ok {
				return rule("bad-undo", "missing output")
			}
			state.DelUTXO(op)
		}
		if len(undo.Spent[i]) != len(tx.Inputs) {
			return rule("bad-undo", "spent count mismatch")
		}
		for j := len(tx.Inputs) - 1; j >= 0; j-- {
			state.PutUTXO(tx.Inputs[j].Prev, undo.Spent[i][j])
		}
		state.DelTx(id)
	}
	return nil
}

// NextTimestamp is the timestamp a miner should stamp on a block extending
// the given ancestor times, given the node's clock.
func NextTimestamp(now int64, ancestorTimes []uint64) (uint64, error) {
	if now < 0 {
		return 0, rule("bad-time", "clock")
	}
	ts := uint64(now)
	if len(ancestorTimes) > 0 {
		mtp := MedianTime(ancestorTimes)
		if ts <= mtp {
			ts = mtp + 1
		}
	}
	if int64(ts) > now+MaxFutureSeconds {
		return 0, rule("bad-time", "median time is too far ahead of the clock")
	}
	return ts, nil
}

// WorkOf decodes bits and returns the block's chainwork contribution.
func WorkOf(bits uint32) (*big.Int, error) {
	target, err := CompactToTarget(bits)
	if err != nil {
		return nil, err
	}
	return BlockWork(target), nil
}
