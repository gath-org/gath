package chain

import (
	"errors"
	"fmt"
	"math/big"

	bolt "go.etcd.io/bbolt"

	"github.com/gath-org/gath/internal/consensus"
)

func (c *Chain) bootstrap() error {
	err := c.db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bkMeta, bkBlocks, bkIndex, bkHeight, bkUTXO, bkAddr, bkUndo, bkTx} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var fresh bool
	err = c.db.View(func(tx *bolt.Tx) error {
		fresh = tx.Bucket(bkMeta).Get(metaTip) == nil
		return nil
	})
	if err != nil {
		return err
	}
	if fresh {
		if err := c.writeGenesis(); err != nil {
			return err
		}
	}
	if err := c.loadIndex(); err != nil {
		return err
	}
	return c.checkGenesis()
}

func (c *Chain) writeGenesis() error {
	g := c.params.Genesis()
	target, err := consensus.CompactToTarget(g.Header.Bits)
	if err != nil {
		return err
	}
	if !consensus.HashMeetsTarget(g.Hash(), target) {
		return errors.New("genesis block does not meet its proof of work")
	}
	if len(g.Txs[0].Outputs) != 0 {
		return errors.New("genesis block has spendable outputs")
	}
	work, err := consensus.WorkOf(g.Header.Bits)
	if err != nil {
		return err
	}
	node := &blkNode{
		hash:   g.Hash(),
		header: g.Header,
		height: 0,
		work:   work,
	}
	undo := &consensus.Undo{Spent: [][]consensus.UTXO{nil}}
	return c.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bkBlocks).Put(node.hash[:], g.Bytes()); err != nil {
			return err
		}
		if err := tx.Bucket(bkUndo).Put(node.hash[:], undo.Bytes()); err != nil {
			return err
		}
		if err := tx.Bucket(bkIndex).Put(node.hash[:], encodeNode(node)); err != nil {
			return err
		}
		if err := tx.Bucket(bkHeight).Put(heightKey(0), node.hash[:]); err != nil {
			return err
		}
		if err := putTxIndex(tx, g.Txs[0].ID(), node.hash, 0, 0); err != nil {
			return err
		}
		if err := tx.Bucket(bkMeta).Put(metaTip, node.hash[:]); err != nil {
			return err
		}
		if err := tx.Bucket(bkMeta).Put(metaGenesis, node.hash[:]); err != nil {
			return err
		}
		return tx.Bucket(bkMeta).Put(metaNetwork, []byte(c.params.Name))
	})
}

func (c *Chain) loadIndex() error {
	err := c.db.View(func(tx *bolt.Tx) error {
		err := tx.Bucket(bkIndex).ForEach(func(k, v []byte) error {
			var hash consensus.Hash
			if len(k) != len(hash) {
				return errors.New("bad index key")
			}
			copy(hash[:], k)
			node, err := decodeNode(hash, v)
			if err != nil {
				return err
			}
			if node.header.Hash() != hash {
				return fmt.Errorf("index hash mismatch for %s", hash)
			}
			if node.work == nil {
				node.work = new(big.Int)
			}
			c.nodes[hash] = node
			return nil
		})
		if err != nil {
			return err
		}
		err = tx.Bucket(bkHeight).ForEach(func(k, v []byte) error {
			if len(k) != 8 || len(v) != 32 {
				return errors.New("bad height index")
			}
			h := uint64(k[0])<<56 | uint64(k[1])<<48 | uint64(k[2])<<40 | uint64(k[3])<<32 |
				uint64(k[4])<<24 | uint64(k[5])<<16 | uint64(k[6])<<8 | uint64(k[7])
			var hash consensus.Hash
			copy(hash[:], v)
			c.byHeight[h] = hash
			return nil
		})
		if err != nil {
			return err
		}
		tip := tx.Bucket(bkMeta).Get(metaTip)
		if len(tip) != 32 {
			return errors.New("missing tip")
		}
		copy(c.tip[:], tip)
		return nil
	})
	if err != nil {
		return err
	}
	tip := c.nodes[c.tip]
	if tip == nil {
		return errors.New("tip is not in the index")
	}
	c.tipWork = new(big.Int).Set(tip.work)
	// Walk the active chain back to genesis and require the height index to agree.
	for n := tip; ; {
		got, ok := c.byHeight[n.height]
		if !ok || got != n.hash {
			return fmt.Errorf("height %d does not point at the active chain", n.height)
		}
		if n.height == 0 {
			if !n.header.Prev.IsZero() {
				return errors.New("genesis has a parent")
			}
			break
		}
		parent := c.nodes[n.header.Prev]
		if parent == nil || parent.height+1 != n.height {
			return fmt.Errorf("broken link at height %d", n.height)
		}
		n = parent
	}
	return nil
}

func (c *Chain) checkGenesis() error {
	want := c.params.GenesisHash()
	got, ok := c.byHeight[0]
	if !ok || got != want {
		return fmt.Errorf("database genesis does not match the %s network", c.params.Name)
	}
	var network string
	err := c.db.View(func(tx *bolt.Tx) error {
		network = string(tx.Bucket(bkMeta).Get(metaNetwork))
		return nil
	})
	if err != nil {
		return err
	}
	if network != c.params.Name {
		return fmt.Errorf("database is %s, not %s", network, c.params.Name)
	}
	return nil
}

// LookupUTXO reads one unspent output from the active chain.
func (c *Chain) LookupUTXO(op consensus.Outpoint) (consensus.UTXO, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var (
		u  consensus.UTXO
		ok bool
	)
	err := c.db.View(func(tx *bolt.Tx) error {
		var err error
		u, ok, err = readUTXOTx(tx, op)
		return err
	})
	return u, ok, err
}

// HasTx reports whether the transaction is in the active chain.
func (c *Chain) HasTx(id consensus.Hash) (bool, error) {
	_, ok, err := c.FindTx(id)
	return ok, err
}

// TipHeight is the height of the active tip.
func (c *Chain) TipHeight() uint64 {
	_, h := c.Tip()
	return h
}
