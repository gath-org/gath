package node

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/gath-org/gath/internal/address"
	"github.com/gath-org/gath/internal/consensus"
)

func (n *Node) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", n.handleStatus)
	mux.HandleFunc("GET /v1/block/tip", n.handleTip)
	mux.HandleFunc("GET /v1/block/{hash}", n.handleBlock)
	mux.HandleFunc("GET /v1/height/{height}", n.handleHeight)
	mux.HandleFunc("GET /v1/tx/{txid}", n.handleTx)
	mux.HandleFunc("GET /v1/address/{addr}/history", n.handleHistory)
	mux.HandleFunc("GET /v1/address/{addr}", n.handleAddress)
	mux.HandleFunc("GET /v1/mempool", n.handleMempool)
	mux.HandleFunc("GET /v1/peers", n.handlePeers)
	mux.HandleFunc("GET /v1/feerate", n.handleFeerate)
	mux.HandleFunc("GET /v1/template", n.handleTemplate)
	mux.HandleFunc("POST /v1/tx", n.handleSubmitTx)
	mux.HandleFunc("POST /v1/block", n.handleSubmitBlock)
	mux.HandleFunc("POST /v1/generate", n.handleGenerate)
	return mux
}

func (n *Node) handleStatus(w http.ResponseWriter, r *http.Request) {
	tip, height := n.chain.Tip()
	hdr, _, ok := n.chain.Header(tip)
	diff := "0"
	if ok {
		target, err := consensus.CompactToTarget(hdr.Bits)
		if err == nil {
			diff = consensus.Difficulty(target, n.params.PowLimit())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"network":    n.params.Name,
		"height":     height,
		"tip":        tip.String(),
		"genesis":    n.chain.GenesisHash().String(),
		"difficulty": diff,
		"peers":      n.net.PeerCount(),
		"mempool":    n.pool.Len(),
		"version":    Software(),
		"protocol":   Protocol(),
	})
}

func (n *Node) handleTip(w http.ResponseWriter, r *http.Request) {
	tip, _ := n.chain.Tip()
	n.writeBlock(w, tip)
}

func (n *Node) handleBlock(w http.ResponseWriter, r *http.Request) {
	hash, err := consensus.ParseHash(r.PathValue("hash"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	n.writeBlock(w, hash)
}

func (n *Node) handleHeight(w http.ResponseWriter, r *http.Request) {
	h, err := strconv.ParseUint(r.PathValue("height"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	block, err := n.chain.BlockAtHeight(h)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, blockJSON(block, h))
}

func (n *Node) writeBlock(w http.ResponseWriter, hash consensus.Hash) {
	hdr, height, ok := n.chain.Header(hash)
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("block not found"))
		return
	}
	block, err := n.chain.Block(hash)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	_ = hdr
	writeJSON(w, http.StatusOK, blockJSON(block, height))
}

func blockJSON(block *consensus.Block, height uint64) map[string]any {
	ids := make([]string, len(block.Txs))
	for i, tx := range block.Txs {
		ids[i] = tx.ID().String()
	}
	return map[string]any{
		"hash":    block.Hash().String(),
		"height":  height,
		"version": block.Header.Version,
		"prev":    block.Header.Prev.String(),
		"merkle":  block.Header.Merkle.String(),
		"time":    block.Header.Time,
		"bits":    fmt.Sprintf("%08x", block.Header.Bits),
		"nonce":   block.Header.Nonce,
		"txids":   ids,
	}
}

func (n *Node) handleTx(w http.ResponseWriter, r *http.Request) {
	id, err := consensus.ParseHash(r.PathValue("txid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if tx, ok := n.pool.Get(id); ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"txid":      id.String(),
			"confirmed": false,
			"raw":       hex.EncodeToString(tx.Bytes()),
		})
		return
	}
	loc, ok, err := n.chain.FindTx(id)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("transaction not found"))
		return
	}
	block, err := n.chain.Block(loc.Block)
	if err != nil || int(loc.Index) >= len(block.Txs) {
		writeErr(w, http.StatusNotFound, errors.New("transaction not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"txid":      id.String(),
		"confirmed": true,
		"height":    loc.Height,
		"block":     loc.Block.String(),
		"index":     loc.Index,
		"raw":       hex.EncodeToString(block.Txs[loc.Index].Bytes()),
	})
}

func (n *Node) handleHistory(w http.ResponseWriter, r *http.Request) {
	hist, err := n.AddressHistory(r.PathValue("addr"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, hist)
}

func (n *Node) handleAddress(w http.ResponseWriter, r *http.Request) {
	info, err := n.LookupAddress(r.PathValue("addr"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (n *Node) handleMempool(w http.ResponseWriter, r *http.Request) {
	ids := n.pool.IDs()
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	writeJSON(w, http.StatusOK, map[string]any{"txids": out})
}

func (n *Node) handlePeers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"peers": n.net.Peers()})
}

func (n *Node) handleFeerate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"atoms_per_byte": fmt.Sprintf("%d", consensus.MinFeePerByte),
	})
}

func (n *Node) handleTemplate(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	hrp, pkh, err := address.Decode(addr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if hrp != n.params.HRP {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("address is for %q, this node is %s", hrp, n.params.Name))
		return
	}
	block, err := n.template(pkh)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	_, height := n.chain.Tip()
	target, err := consensus.CompactToTarget(block.Header.Bits)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"height": height + 1,
		"bits":   fmt.Sprintf("%08x", block.Header.Bits),
		"target": padHex(target, 64),
		"raw":    hex.EncodeToString(block.Bytes()),
	})
}

func (n *Node) handleSubmitTx(w http.ResponseWriter, r *http.Request) {
	raw, err := readRaw(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	tx, err := consensus.DecodeTx(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := n.SubmitTx(tx); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"txid": tx.ID().String()})
}

func (n *Node) handleSubmitBlock(w http.ResponseWriter, r *http.Request) {
	raw, err := readRaw(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	block, err := consensus.DecodeBlock(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	acc, err := n.SubmitBlock(block)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hash":     block.Hash().String(),
		"accepted": !acc.AlreadyHad || acc.Tip == block.Hash(),
		"tip":      acc.Tip.String(),
		"height":   acc.Height,
	})
}

func (n *Node) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
		Blocks  int    `json:"blocks"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	hrp, pkh, err := address.Decode(req.Address)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if hrp != n.params.HRP {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("address is for %q, this node is %s", hrp, n.params.Name))
		return
	}
	if err := n.Generate(r.Context(), pkh, req.Blocks); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	tip, height := n.chain.Tip()
	writeJSON(w, http.StatusOK, map[string]any{
		"height": height,
		"tip":    tip.String(),
	})
}

func readRaw(r *http.Request) ([]byte, error) {
	var req struct {
		Raw string `json:"raw"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&req); err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(strings.TrimSpace(req.Raw))
	if err != nil {
		return nil, fmt.Errorf("raw: %w", err)
	}
	return raw, nil
}

func padHex(n *big.Int, width int) string {
	s := n.Text(16)
	if len(s) >= width {
		return s
	}
	return strings.Repeat("0", width-len(s)) + s
}

func statusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if _, ok := consensus.AsRule(err); ok {
		return http.StatusBadRequest
	}
	msg := err.Error()
	if strings.Contains(msg, "not found") || strings.Contains(msg, "not stored") || strings.Contains(msg, "no block") {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
