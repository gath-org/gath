package node

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/gath-org/gath/internal/address"
	"github.com/gath-org/gath/internal/consensus"
)

// HistoryTx is one transaction that pays or spends an address.
type HistoryTx struct {
	TxID          string `json:"txid"`
	Height        uint64 `json:"height"`
	Index         uint32 `json:"index"`
	Confirmed     bool   `json:"confirmed"`
	Time          uint64 `json:"time"`
	ReceivedAtoms string `json:"received_atoms"`
	SentAtoms     string `json:"sent_atoms"`
	NetAtoms      string `json:"net_atoms"`
	FeeAtoms      string `json:"fee_atoms"`
}

// History is the activity of one address, newest first.
// Unconfirmed mempool transactions come before confirmed ones.
type History struct {
	Address      string      `json:"address"`
	Transactions []HistoryTx `json:"transactions"`
}

// AddressHistory walks the active chain and the mempool.
// A full scan is enough for devnet and short chains. It is not an index.
func (n *Node) AddressHistory(addr string) (History, error) {
	hrp, pkh, err := address.Decode(addr)
	if err != nil {
		return History{}, err
	}
	if hrp != n.params.HRP {
		return History{}, fmt.Errorf("address is for %q, this node is %s", hrp, n.params.Name)
	}
	_, tip := n.chain.Tip()
	var rows []histRow
	seen := make(map[consensus.Hash]bool)
	for h := uint64(1); h <= tip; h++ {
		block, err := n.chain.BlockAtHeight(h)
		if err != nil {
			return History{}, err
		}
		for i, tx := range block.Txs {
			row, ok, err := n.movement(tx, pkh, h, uint32(i), block.Header.Time, true)
			if err != nil {
				return History{}, err
			}
			if !ok {
				continue
			}
			seen[tx.ID()] = true
			rows = append(rows, row)
		}
	}
	for _, id := range n.pool.IDs() {
		if seen[id] {
			continue
		}
		tx, ok := n.pool.Get(id)
		if !ok {
			continue
		}
		row, ok, err := n.movement(tx, pkh, 0, 0, 0, false)
		if err != nil {
			return History{}, err
		}
		if ok {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].confirmed != rows[j].confirmed {
			return !rows[i].confirmed
		}
		if rows[i].height != rows[j].height {
			return rows[i].height > rows[j].height
		}
		return rows[i].index > rows[j].index
	})
	out := History{Address: addr, Transactions: make([]HistoryTx, 0, len(rows))}
	for _, row := range rows {
		fee := ""
		if row.feeKnown {
			fee = strconv.FormatUint(row.fee, 10)
		}
		out.Transactions = append(out.Transactions, HistoryTx{
			TxID:          row.txid.String(),
			Height:        row.height,
			Index:         row.index,
			Confirmed:     row.confirmed,
			Time:          row.time,
			ReceivedAtoms: strconv.FormatUint(row.received, 10),
			SentAtoms:     strconv.FormatUint(row.sent, 10),
			NetAtoms:      formatSigned(int64(row.received) - int64(row.sent)),
			FeeAtoms:      fee,
		})
	}
	return out, nil
}

type histRow struct {
	txid      consensus.Hash
	height    uint64
	index     uint32
	time      uint64
	confirmed bool
	received  uint64
	sent      uint64
	fee       uint64
	feeKnown  bool
}

func (n *Node) movement(tx *consensus.Tx, pkh consensus.Hash, height uint64, index uint32, when uint64, confirmed bool) (histRow, bool, error) {
	var received, sent, inSum, outSum uint64
	allOurs := true
	for i := range tx.Inputs {
		in := &tx.Inputs[i]
		if in.Prev.IsCoinbase() || len(in.PubKey) == 0 {
			allOurs = false
			continue
		}
		prev, ok, err := n.lookupOutput(in.Prev)
		if err != nil {
			return histRow{}, false, err
		}
		if !ok {
			return histRow{}, false, fmt.Errorf("missing input %s:%d", in.Prev.Hash, in.Prev.Index)
		}
		inSum += prev.Value
		if consensus.PubKeyHash(in.PubKey) == pkh {
			sent += prev.Value
		} else {
			allOurs = false
		}
	}
	for i := range tx.Outputs {
		outSum += tx.Outputs[i].Value
		if tx.Outputs[i].PKH == pkh {
			received += tx.Outputs[i].Value
		}
	}
	if received == 0 && sent == 0 {
		return histRow{}, false, nil
	}
	row := histRow{
		txid:      tx.ID(),
		height:    height,
		index:     index,
		time:      when,
		confirmed: confirmed,
		received:  received,
		sent:      sent,
	}
	if allOurs && sent > 0 && inSum >= outSum {
		row.fee = inSum - outSum
		row.feeKnown = true
	}
	return row, true, nil
}

func (n *Node) lookupOutput(op consensus.Outpoint) (consensus.TxOut, bool, error) {
	if tx, ok := n.pool.Get(op.Hash); ok {
		if int(op.Index) < len(tx.Outputs) {
			return tx.Outputs[op.Index], true, nil
		}
		return consensus.TxOut{}, false, nil
	}
	loc, ok, err := n.chain.FindTx(op.Hash)
	if err != nil || !ok {
		return consensus.TxOut{}, false, err
	}
	block, err := n.chain.Block(loc.Block)
	if err != nil {
		return consensus.TxOut{}, false, err
	}
	if int(loc.Index) >= len(block.Txs) {
		return consensus.TxOut{}, false, nil
	}
	tx := block.Txs[loc.Index]
	if int(op.Index) >= len(tx.Outputs) {
		return consensus.TxOut{}, false, nil
	}
	return tx.Outputs[op.Index], true, nil
}

func formatSigned(n int64) string {
	if n < 0 {
		return "-" + strconv.FormatUint(uint64(-n), 10)
	}
	return strconv.FormatUint(uint64(n), 10)
}
