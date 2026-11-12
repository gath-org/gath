package chain

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/gath-org/gath/internal/consensus"
)

const maxReorgDepth = 10000

// ErrOrphan means the block's parent is not in the store.
var ErrOrphan = errors.New("unknown parent")

// Accept is the result of processing a block.
type Accept struct {
	AlreadyHad bool
	TipChanged bool
	Height     uint64
	Tip        consensus.Hash
	// Removed are blocks that left the active chain, tip first.
	Removed []*consensus.Block
	// Added are blocks that entered the active chain, oldest first.
	Added []*consensus.Block
}

type blkNode struct {
	hash   consensus.Hash
	header consensus.BlockHeader
	height uint64
	work   *big.Int
}

// Chain is a block store, the active UTXO set, and reorg handling.
type Chain struct {
	db       *bolt.DB
	params   *consensus.Params
	mu       sync.Mutex
	nodes    map[consensus.Hash]*blkNode
	byHeight map[uint64]consensus.Hash
	tip      consensus.Hash
	tipWork  *big.Int
	now      func() time.Time
}

// Open opens or creates a chain database for the given network.
func Open(path string, params *consensus.Params) (*Chain, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	c := &Chain{
		db:       db,
		params:   params,
		nodes:    make(map[consensus.Hash]*blkNode),
		byHeight: make(map[uint64]consensus.Hash),
		now:      time.Now,
	}
	if err := c.bootstrap(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return c, nil
}

// Close closes the database.
func (c *Chain) Close() error { return c.db.Close() }

// SetClock overrides the clock used for the future-time rule. Tests use it.
func (c *Chain) SetClock(now func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

// Params returns the network parameters.
func (c *Chain) Params() *consensus.Params { return c.params }

// Tip returns the active tip hash and height.
func (c *Chain) Tip() (consensus.Hash, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.nodes[c.tip]
	return c.tip, n.height
}

// TipWork returns a copy of the cumulative work of the tip.
func (c *Chain) TipWork() *big.Int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return new(big.Int).Set(c.tipWork)
}

// GenesisHash is the network genesis hash.
func (c *Chain) GenesisHash() consensus.Hash { return c.params.GenesisHash() }

// Block returns a stored block.
func (c *Chain) Block(hash consensus.Hash) (*consensus.Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loadBlock(hash)
}

// BlockAtHeight returns the active block at height.
func (c *Chain) BlockAtHeight(height uint64) (*consensus.Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	hash, ok := c.byHeight[height]
	if !ok {
		return nil, fmt.Errorf("no block at height %d", height)
	}
	return c.loadBlock(hash)
}

// Header returns a stored header.
func (c *Chain) Header(hash consensus.Hash) (consensus.BlockHeader, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.nodes[hash]
	if !ok {
		return consensus.BlockHeader{}, 0, false
	}
	return n.header, n.height, true
}

// HasBlock reports whether the block body is stored.
func (c *Chain) HasBlock(hash consensus.Hash) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.nodes[hash]
	return ok
}

// TxLoc is where a confirmed transaction lives.
type TxLoc struct {
	Block  consensus.Hash
	Index  uint32
	Height uint64
}

// FindTx looks up a transaction in the active chain.
func (c *Chain) FindTx(id consensus.Hash) (TxLoc, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var loc TxLoc
	var ok bool
	err := c.db.View(func(tx *bolt.Tx) error {
		block, index, height, found, err := readTxIndex(tx, id)
		if err != nil || !found {
			return err
		}
		loc = TxLoc{Block: block, Index: index, Height: height}
		ok = true
		return nil
	})
	return loc, ok, err
}

// UTXOInfo is an unspent output plus its outpoint.
type UTXOInfo struct {
	Outpoint consensus.Outpoint
	UTXO     consensus.UTXO
}

// UTXOsByAddress returns every unspent output paying pkh.
func (c *Chain) UTXOsByAddress(pkh consensus.Hash) ([]UTXOInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []UTXOInfo
	err := c.db.View(func(tx *bolt.Tx) error {
		cur := tx.Bucket(bkAddr).Cursor()
		prefix := pkh[:]
		for k, v := cur.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = cur.Next() {
			if len(k) != 32+36 {
				return errors.New("bad address index key")
			}
			var op consensus.Outpoint
			copy(op.Hash[:], k[32:64])
			op.Index = binaryUint32(k[64:68])
			u, err := decodeUTXO(v)
			if err != nil {
				return err
			}
			out = append(out, UTXOInfo{Outpoint: op, UTXO: u})
		}
		return nil
	})
	return out, err
}

func binaryUint32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// Locator returns block hashes a peer can use to find a common ancestor.
func (c *Chain) Locator() []consensus.Hash {
	c.mu.Lock()
	defer c.mu.Unlock()
	var loc []consensus.Hash
	var step uint64 = 1
	h := c.nodes[c.tip].height
	for len(loc) < 32 {
		hash, ok := c.byHeight[h]
		if !ok {
			break
		}
		loc = append(loc, hash)
		if h == 0 {
			break
		}
		if h < step {
			h = 0
		} else {
			h -= step
		}
		if len(loc) > 8 {
			step *= 2
		}
	}
	return loc
}

// Locate returns up to limit active-chain hashes after the first locator
// that is on the active chain, stopping at stop if it is non-zero.
func (c *Chain) Locate(locators []consensus.Hash, stop consensus.Hash, limit int) []consensus.Hash {
	c.mu.Lock()
	defer c.mu.Unlock()
	if limit <= 0 {
		return nil
	}
	start := uint64(0)
	for _, l := range locators {
		n, ok := c.nodes[l]
		if !ok || !c.active(n) {
			continue
		}
		start = n.height
		break
	}
	var out []consensus.Hash
	tipH := c.nodes[c.tip].height
	for h := start + 1; h <= tipH && len(out) < limit; h++ {
		hash := c.byHeight[h]
		out = append(out, hash)
		if stop != (consensus.Hash{}) && hash == stop {
			break
		}
	}
	return out
}

// Template builds a block on the tip without a proof of work.
// fees is the sum of fees of txs, which must already be valid against the tip
// once the coinbase is prepended. The caller is the mempool.
func (c *Chain) Template(payTo consensus.Hash, timestamp uint64, txs []*consensus.Tx, fees uint64) (*consensus.Block, error) {
	return c.BuildOn(consensus.Hash{}, payTo, timestamp, txs, fees)
}

// BuildOn builds a block extending parent. A zero parent means the tip.
// The block has no proof of work yet.
func (c *Chain) BuildOn(parent consensus.Hash, payTo consensus.Hash, timestamp uint64, txs []*consensus.Tx, fees uint64) (*consensus.Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if parent.IsZero() {
		parent = c.tip
	}
	return c.buildLocked(parent, payTo, timestamp, txs, fees)
}

func (c *Chain) buildLocked(parentHash consensus.Hash, payTo consensus.Hash, timestamp uint64, txs []*consensus.Tx, fees uint64) (*consensus.Block, error) {
	parent := c.nodes[parentHash]
	if parent == nil {
		return nil, fmt.Errorf("unknown parent %s", parentHash)
	}
	height := parent.height + 1
	subsidy := consensus.Subsidy(height)
	value, err := addFee(subsidy, fees)
	if err != nil {
		return nil, err
	}
	cb, err := consensus.NewCoinbase(height, value, payTo, 0, []byte("gath"))
	if err != nil {
		return nil, err
	}
	all := make([]*consensus.Tx, 0, 1+len(txs))
	all = append(all, cb)
	all = append(all, txs...)
	ids := make([]consensus.Hash, len(all))
	for i, tx := range all {
		ids[i] = tx.ID()
	}
	var gp *consensus.BlockHeader
	if parent.height > 0 {
		g := c.nodes[parent.header.Prev]
		if g == nil {
			return nil, errors.New("missing grandparent")
		}
		gp = &g.header
	}
	bits, err := consensus.NextBits(&parent.header, parent.height, gp, c.params)
	if err != nil {
		return nil, err
	}
	ts := timestamp
	if ts == 0 {
		ts, err = consensus.NextTimestamp(c.now().Unix(), c.timesOf(parent))
		if err != nil {
			return nil, err
		}
	}
	block := &consensus.Block{
		Header: consensus.BlockHeader{
			Version: consensus.BlockVersion,
			Prev:    parent.hash,
			Merkle:  consensus.MerkleRoot(ids),
			Time:    ts,
			Bits:    bits,
		},
		Txs: all,
	}
	if len(block.Bytes()) > consensus.MaxBlockBytes {
		return nil, fmt.Errorf("template exceeds the block size limit")
	}
	return block, nil
}

func addFee(a, b uint64) (uint64, error) {
	if a > ^uint64(0)-b {
		return 0, errors.New("fee overflow")
	}
	return a + b, nil
}

// Process validates a block and stores it. If it extends the heaviest chain,
// it becomes the tip.
func (c *Chain) Process(block *consensus.Block) (Accept, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.process(block)
}

func (c *Chain) process(block *consensus.Block) (Accept, error) {
	hash := block.Hash()
	if _, ok := c.nodes[hash]; ok {
		return Accept{AlreadyHad: true, Height: c.nodes[c.tip].height, Tip: c.tip}, nil
	}
	parent, ok := c.nodes[block.Header.Prev]
	if !ok {
		return Accept{}, ErrOrphan
	}
	blockWork, err := consensus.WorkOf(block.Header.Bits)
	if err != nil {
		return Accept{}, err
	}
	newWork := new(big.Int).Add(parent.work, blockWork)
	heavier := newWork.Cmp(c.tipWork) > 0

	lca, err := c.common(c.nodes[c.tip], parent)
	if err != nil {
		return Accept{}, err
	}
	discNodes := c.down(c.nodes[c.tip], lca)
	if len(discNodes) > maxReorgDepth {
		return Accept{}, fmt.Errorf("reorg of %d blocks exceeds the limit", len(discNodes))
	}
	if !heavier && len(discNodes) > 64 {
		return Accept{}, fmt.Errorf("side branch is %d blocks deep", len(discNodes))
	}

	st, err := c.stateAt(parent, lca, discNodes)
	if err != nil {
		return Accept{}, err
	}
	var gp *consensus.BlockHeader
	if parent.height > 0 {
		g, ok := c.nodes[parent.header.Prev]
		if !ok {
			return Accept{}, errors.New("missing grandparent")
		}
		gp = &g.header
	}
	undo, err := consensus.ConnectBlock(c.params, block, &parent.header, parent.height, gp, c.timesOf(parent), c.now().Unix(), true, st)
	if err != nil {
		return Accept{}, err
	}
	if st.err != nil {
		return Accept{}, st.err
	}

	node := &blkNode{
		hash:   hash,
		header: block.Header,
		height: parent.height + 1,
		work:   newWork,
	}
	var removed, added []*consensus.Block
	var side []*blkNode
	if heavier {
		side = c.up(lca, parent)
		removed, err = c.blocksOf(discNodes)
		if err != nil {
			return Accept{}, err
		}
		added, err = c.blocksOf(side)
		if err != nil {
			return Accept{}, err
		}
		added = append(added, block)
	}

	err = c.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bkBlocks).Put(hash[:], block.Bytes()); err != nil {
			return err
		}
		if err := tx.Bucket(bkUndo).Put(hash[:], undo.Bytes()); err != nil {
			return err
		}
		if err := tx.Bucket(bkIndex).Put(hash[:], encodeNode(node)); err != nil {
			return err
		}
		if !heavier {
			return nil
		}
		if err := c.applyDiff(tx, st); err != nil {
			return err
		}
		if err := c.rewriteTxs(tx, discNodes, side, block, node.height); err != nil {
			return err
		}
		if err := c.rewriteHeights(tx, lca.height, discNodes, side, node); err != nil {
			return err
		}
		if err := tx.Bucket(bkMeta).Put(metaTip, hash[:]); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Accept{}, err
	}
	c.nodes[hash] = node
	acc := Accept{Height: c.nodes[c.tip].height, Tip: c.tip}
	if heavier {
		for _, n := range discNodes {
			delete(c.byHeight, n.height)
		}
		for _, n := range side {
			c.byHeight[n.height] = n.hash
		}
		c.byHeight[node.height] = hash
		// Drop heights the shorter chain no longer has.
		oldHeight := c.nodes[c.tip].height
		for h := node.height + 1; h <= oldHeight; h++ {
			delete(c.byHeight, h)
		}
		c.tip = hash
		c.tipWork = newWork
		acc.TipChanged = true
		acc.Height = node.height
		acc.Tip = hash
		acc.Removed = removed
		acc.Added = added
	}
	return acc, nil
}

func (c *Chain) active(n *blkNode) bool {
	h, ok := c.byHeight[n.height]
	return ok && h == n.hash
}

func (c *Chain) timesOf(n *blkNode) []uint64 {
	var times []uint64
	for n != nil && len(times) < consensus.MedianTimeBlocks {
		times = append(times, n.header.Time)
		if n.height == 0 {
			break
		}
		n = c.nodes[n.header.Prev]
	}
	return times
}

func (c *Chain) common(a, b *blkNode) (*blkNode, error) {
	for a.height > b.height {
		next, ok := c.nodes[a.header.Prev]
		if !ok {
			return nil, errors.New("broken link")
		}
		a = next
	}
	for b.height > a.height {
		next, ok := c.nodes[b.header.Prev]
		if !ok {
			return nil, errors.New("broken link")
		}
		b = next
	}
	for a.hash != b.hash {
		var ok bool
		a, ok = c.nodes[a.header.Prev]
		if !ok {
			return nil, errors.New("broken link")
		}
		b, ok = c.nodes[b.header.Prev]
		if !ok {
			return nil, errors.New("broken link")
		}
	}
	return a, nil
}

// down returns nodes from 'from' back until but not including stop, tip first.
func (c *Chain) down(from, stop *blkNode) []*blkNode {
	var out []*blkNode
	for from != nil && from.hash != stop.hash {
		out = append(out, from)
		if from.height == 0 {
			break
		}
		from = c.nodes[from.header.Prev]
	}
	return out
}

// up returns nodes from just after start through end, oldest first.
func (c *Chain) up(start, end *blkNode) []*blkNode {
	var rev []*blkNode
	for end != nil && end.hash != start.hash {
		rev = append(rev, end)
		if end.height == 0 {
			break
		}
		end = c.nodes[end.header.Prev]
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

func (c *Chain) stateAt(parent, lca *blkNode, disc []*blkNode) (*memState, error) {
	st := newMemState(c)
	// disc is tip-first down to the child of lca. Undo that prefix only when
	// the parent branch leaves the active chain at lca. If parent is on the
	// active chain, lca is parent (or an ancestor — actually common(tip, parent)
	// is parent when parent is active). Undo exactly disc.
	for _, n := range disc {
		block, undo, err := c.loadBlockUndo(n.hash)
		if err != nil {
			return nil, err
		}
		if err := consensus.UndoBlock(block, undo, st); err != nil {
			return nil, err
		}
		if st.err != nil {
			return nil, st.err
		}
	}
	side := c.up(lca, parent)
	for _, n := range side {
		block, undo, err := c.loadBlockUndo(n.hash)
		if err != nil {
			return nil, err
		}
		if err := consensus.ReplayBlock(block, undo, n.height, st); err != nil {
			return nil, err
		}
		if st.err != nil {
			return nil, st.err
		}
	}
	return st, nil
}

func (c *Chain) loadBlock(hash consensus.Hash) (*consensus.Block, error) {
	var raw []byte
	err := c.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bkBlocks).Get(hash[:])
		if v == nil {
			return fmt.Errorf("block %s is not stored", hash)
		}
		raw = append([]byte(nil), v...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return consensus.DecodeBlock(raw)
}

func (c *Chain) loadBlockUndo(hash consensus.Hash) (*consensus.Block, *consensus.Undo, error) {
	block, err := c.loadBlock(hash)
	if err != nil {
		return nil, nil, err
	}
	var raw []byte
	err = c.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bkUndo).Get(hash[:])
		if v == nil {
			return fmt.Errorf("undo for %s is not stored", hash)
		}
		raw = append([]byte(nil), v...)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	undo, err := consensus.DecodeUndo(raw)
	if err != nil {
		return nil, nil, err
	}
	return block, undo, nil
}

func (c *Chain) blocksOf(nodes []*blkNode) ([]*consensus.Block, error) {
	out := make([]*consensus.Block, len(nodes))
	for i, n := range nodes {
		b, err := c.loadBlock(n.hash)
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}

func (c *Chain) applyDiff(tx *bolt.Tx, st *memState) error {
	for op, ch := range st.utxo {
		if ch.present {
			if err := putUTXO(tx, op, ch.utxo); err != nil {
				return err
			}
			continue
		}
		if err := delUTXO(tx, op); err != nil {
			return err
		}
	}
	return nil
}

func (c *Chain) rewriteTxs(tx *bolt.Tx, disc, side []*blkNode, block *consensus.Block, height uint64) error {
	for _, n := range disc {
		b, err := decodeStored(tx, n.hash)
		if err != nil {
			return err
		}
		for _, t := range b.Txs {
			if err := delTxIndex(tx, t.ID()); err != nil {
				return err
			}
		}
	}
	for _, n := range side {
		b, err := decodeStored(tx, n.hash)
		if err != nil {
			return err
		}
		for i, t := range b.Txs {
			if err := putTxIndex(tx, t.ID(), n.hash, uint32(i), n.height); err != nil {
				return err
			}
		}
	}
	for i, t := range block.Txs {
		if err := putTxIndex(tx, t.ID(), block.Hash(), uint32(i), height); err != nil {
			return err
		}
	}
	return nil
}

func decodeStored(tx *bolt.Tx, hash consensus.Hash) (*consensus.Block, error) {
	raw := tx.Bucket(bkBlocks).Get(hash[:])
	if raw == nil {
		return nil, fmt.Errorf("missing block %s", hash)
	}
	return consensus.DecodeBlock(raw)
}

func (c *Chain) rewriteHeights(tx *bolt.Tx, lcaHeight uint64, disc, side []*blkNode, tip *blkNode) error {
	_ = lcaHeight
	_ = disc
	b := tx.Bucket(bkHeight)
	for _, n := range side {
		if err := b.Put(heightKey(n.height), n.hash[:]); err != nil {
			return err
		}
	}
	if err := b.Put(heightKey(tip.height), tip.hash[:]); err != nil {
		return err
	}
	// Delete active heights the new chain does not occupy.
	oldTip := c.nodes[c.tip].height
	for h := tip.height + 1; h <= oldTip; h++ {
		if err := b.Delete(heightKey(h)); err != nil {
			return err
		}
	}
	return nil
}
