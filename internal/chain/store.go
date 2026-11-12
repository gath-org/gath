package chain

import (
	"encoding/binary"
	"errors"
	"math/big"

	bolt "go.etcd.io/bbolt"

	"github.com/gath-org/gath/internal/consensus"
)

var (
	bkMeta   = []byte("meta")
	bkBlocks = []byte("blocks")
	bkIndex  = []byte("index")
	bkHeight = []byte("height")
	bkUTXO   = []byte("utxo")
	bkAddr   = []byte("addr")
	bkUndo   = []byte("undo")
	bkTx     = []byte("tx")

	metaTip     = []byte("tip")
	metaNetwork = []byte("network")
	metaGenesis = []byte("genesis")
)

func heightKey(h uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], h)
	return b[:]
}

func outpointKey(op consensus.Outpoint) []byte {
	b := make([]byte, 36)
	copy(b, op.Hash[:])
	binary.LittleEndian.PutUint32(b[32:], op.Index)
	return b
}

func addrKey(pkh consensus.Hash, op consensus.Outpoint) []byte {
	b := make([]byte, 32+36)
	copy(b, pkh[:])
	copy(b[32:], outpointKey(op))
	return b
}

func putUTXO(tx *bolt.Tx, op consensus.Outpoint, u consensus.UTXO) error {
	w := consensus.W{}
	w.U64(u.Value)
	w.Hash(u.PKH)
	w.U64(u.Height)
	if u.Coinbase {
		w.U8(1)
	} else {
		w.U8(0)
	}
	raw := w.Bytes()
	if err := tx.Bucket(bkUTXO).Put(outpointKey(op), raw); err != nil {
		return err
	}
	return tx.Bucket(bkAddr).Put(addrKey(u.PKH, op), raw)
}

func delUTXO(tx *bolt.Tx, op consensus.Outpoint) error {
	key := outpointKey(op)
	raw := tx.Bucket(bkUTXO).Get(key)
	if raw == nil {
		return tx.Bucket(bkUTXO).Delete(key)
	}
	u, err := decodeUTXO(raw)
	if err != nil {
		return err
	}
	if err := tx.Bucket(bkAddr).Delete(addrKey(u.PKH, op)); err != nil {
		return err
	}
	return tx.Bucket(bkUTXO).Delete(key)
}

func readUTXOTx(tx *bolt.Tx, op consensus.Outpoint) (consensus.UTXO, bool, error) {
	raw := tx.Bucket(bkUTXO).Get(outpointKey(op))
	if raw == nil {
		return consensus.UTXO{}, false, nil
	}
	u, err := decodeUTXO(raw)
	if err != nil {
		return consensus.UTXO{}, false, err
	}
	return u, true, nil
}

func decodeUTXO(raw []byte) (consensus.UTXO, error) {
	r := consensus.NewReader(raw)
	var u consensus.UTXO
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
	case 1:
		u.Coinbase = true
	default:
		return u, errors.New("bad utxo flag")
	}
	if err := r.Empty(); err != nil {
		return u, err
	}
	return u, nil
}

func encodeNode(n *blkNode) []byte {
	w := consensus.W{}
	w.Raw(n.header.Bytes())
	w.U64(n.height)
	wb := n.work.Bytes()
	w.VarInt(uint64(len(wb)))
	w.Raw(wb)
	return w.Bytes()
}

func decodeNode(hash consensus.Hash, raw []byte) (*blkNode, error) {
	r := consensus.NewReader(raw)
	if r.Remaining() < consensus.HeaderLen {
		return nil, errors.New("short index record")
	}
	hdrBytes, err := r.Raw(consensus.HeaderLen)
	if err != nil {
		return nil, err
	}
	hdr, err := decodeHeader(hdrBytes)
	if err != nil {
		return nil, err
	}
	height, err := r.U64()
	if err != nil {
		return nil, err
	}
	n, err := r.VarInt()
	if err != nil {
		return nil, err
	}
	wb, err := r.Raw(int(n))
	if err != nil {
		return nil, err
	}
	if err := r.Empty(); err != nil {
		return nil, err
	}
	work := new(big.Int).SetBytes(wb)
	return &blkNode{hash: hash, header: hdr, height: height, work: work}, nil
}

func decodeHeader(b []byte) (consensus.BlockHeader, error) {
	r := consensus.NewReader(b)
	var h consensus.BlockHeader
	var err error
	if h.Version, err = r.U32(); err != nil {
		return h, err
	}
	if h.Prev, err = r.Hash(); err != nil {
		return h, err
	}
	if h.Merkle, err = r.Hash(); err != nil {
		return h, err
	}
	if h.Time, err = r.U64(); err != nil {
		return h, err
	}
	if h.Bits, err = r.U32(); err != nil {
		return h, err
	}
	if h.Nonce, err = r.U64(); err != nil {
		return h, err
	}
	return h, r.Empty()
}

func putTxIndex(tx *bolt.Tx, id, block consensus.Hash, index uint32, height uint64) error {
	w := consensus.W{}
	w.Hash(block)
	w.U32(index)
	w.U64(height)
	return tx.Bucket(bkTx).Put(id[:], w.Bytes())
}

func delTxIndex(tx *bolt.Tx, id consensus.Hash) error {
	return tx.Bucket(bkTx).Delete(id[:])
}

func readTxIndex(tx *bolt.Tx, id consensus.Hash) (block consensus.Hash, index uint32, height uint64, ok bool, err error) {
	raw := tx.Bucket(bkTx).Get(id[:])
	if raw == nil {
		return consensus.Hash{}, 0, 0, false, nil
	}
	r := consensus.NewReader(raw)
	if block, err = r.Hash(); err != nil {
		return consensus.Hash{}, 0, 0, false, err
	}
	if index, err = r.U32(); err != nil {
		return consensus.Hash{}, 0, 0, false, err
	}
	if height, err = r.U64(); err != nil {
		return consensus.Hash{}, 0, 0, false, err
	}
	return block, index, height, true, nil
}
