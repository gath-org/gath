package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gath-org/gath/internal/consensus"
)

// Client talks to a node's local JSON API.
type Client struct {
	Base string
	HTTP *http.Client
}

// New returns a client for a base URL such as http://127.0.0.1:19734.
func New(base string) *Client {
	base = strings.TrimRight(base, "/")
	return &Client{
		Base: base,
		HTTP: &http.Client{Timeout: 10 * time.Minute},
	}
}

// Status is the node summary.
type Status struct {
	Network    string `json:"network"`
	Height     uint64 `json:"height"`
	Tip        string `json:"tip"`
	Genesis    string `json:"genesis"`
	Difficulty string `json:"difficulty"`
	Peers      int    `json:"peers"`
	Mempool    int    `json:"mempool"`
	Version    string `json:"version"`
	Protocol   uint32 `json:"protocol"`
}

// UTXO is one output from the address endpoint.
type UTXO struct {
	TxID     string `json:"txid"`
	Index    uint32 `json:"index"`
	Atoms    string `json:"atoms"`
	Height   uint64 `json:"height"`
	Coinbase bool   `json:"coinbase"`
	Mature   bool   `json:"mature"`
	Reserved bool   `json:"reserved"`
}

// Address is the address endpoint.
type Address struct {
	Address        string `json:"address"`
	Confirmed      string `json:"confirmed"`
	ConfirmedAtoms string `json:"confirmed_atoms"`
	Spendable      string `json:"spendable"`
	SpendableAtoms string `json:"spendable_atoms"`
	UTXOs          []UTXO `json:"utxos"`
}

// Peer is one completed handshake from /v1/peers.
type Peer struct {
	Addr     string `json:"addr"`
	Agent    string `json:"agent"`
	Height   uint64 `json:"height"`
	Outbound bool   `json:"outbound"`
}

// Peers fetches the node's current peers.
func (c *Client) Peers(ctx context.Context) ([]Peer, error) {
	var out struct {
		Peers []Peer `json:"peers"`
	}
	if err := c.get(ctx, "/v1/peers", &out); err != nil {
		return nil, err
	}
	if out.Peers == nil {
		return []Peer{}, nil
	}
	return out.Peers, nil
}

// Status fetches /v1/status.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var out Status
	err := c.get(ctx, "/v1/status", &out)
	return out, err
}

// HistoryTx is one wallet movement from the node.
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

// History is the address history endpoint.
type History struct {
	Address      string      `json:"address"`
	Transactions []HistoryTx `json:"transactions"`
}

// Address fetches confirmed outputs for addr.
func (c *Client) Address(ctx context.Context, addr string) (Address, error) {
	var out Address
	err := c.get(ctx, "/v1/address/"+addr, &out)
	return out, err
}

// History fetches transactions that pay or spend addr.
func (c *Client) History(ctx context.Context, addr string) (History, error) {
	var out History
	err := c.get(ctx, "/v1/address/"+addr+"/history", &out)
	return out, err
}

// FeeRate fetches the consensus minimum fee rate in atoms per byte.
func (c *Client) FeeRate(ctx context.Context) (uint64, error) {
	var out struct {
		Atoms string `json:"atoms_per_byte"`
	}
	if err := c.get(ctx, "/v1/feerate", &out); err != nil {
		return 0, err
	}
	var n uint64
	if _, err := fmt.Sscan(out.Atoms, &n); err != nil {
		return 0, err
	}
	return n, nil
}

// SubmitTx posts a transaction. The returned id is the txid.
func (c *Client) SubmitTx(ctx context.Context, tx *consensus.Tx) (string, error) {
	var out struct {
		TxID string `json:"txid"`
	}
	err := c.post(ctx, "/v1/tx", map[string]string{
		"raw": hex.EncodeToString(tx.Bytes()),
	}, &out)
	return out.TxID, err
}

// Template fetches a block template paying address.
func (c *Client) Template(ctx context.Context, addr string) (*consensus.Block, error) {
	var out struct {
		Raw string `json:"raw"`
	}
	if err := c.get(ctx, "/v1/template?address="+addr, &out); err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(out.Raw)
	if err != nil {
		return nil, err
	}
	return consensus.DecodeBlock(raw)
}

// SubmitBlock posts a mined block.
func (c *Client) SubmitBlock(ctx context.Context, block *consensus.Block) error {
	return c.post(ctx, "/v1/block", map[string]string{
		"raw": hex.EncodeToString(block.Bytes()),
	}, nil)
}

// Generate asks the node to mine blocks.
func (c *Client) Generate(ctx context.Context, addr string, blocks int) error {
	return c.post(ctx, "/v1/generate", map[string]any{
		"address": addr,
		"blocks":  blocks,
	}, nil)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var env struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &env) == nil && env.Error != "" {
			return fmt.Errorf("%s", env.Error)
		}
		return fmt.Errorf("node returned %s", resp.Status)
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
}
