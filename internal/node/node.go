package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gath-org/gath/internal/address"
	"github.com/gath-org/gath/internal/amount"
	"github.com/gath-org/gath/internal/chain"
	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/mempool"
	"github.com/gath-org/gath/internal/miner"
	"github.com/gath-org/gath/internal/p2p"
	"github.com/gath-org/gath/internal/version"
)

const (
	maxOrphans   = 64
	maxGenerate  = 500
	templateRoom = 1000
)

// Config is how a node process is started.
type Config struct {
	Network  string
	DataDir  string
	Listen   string
	RPC      string
	External string
	Connect  []string
	// Seeds are bootnode addresses (host:port) dialed with Connect.
	// DNSSeeds are names resolved to that same peer port.
	// Lookup overrides DNS resolution. Nil uses the system resolver.
	Seeds    []string
	DNSSeeds []string
	Lookup   p2p.LookupIP
	Mine     bool
	MineTo   string
	Threads  int
}

// Node is one full node: chain, mempool, peer network, and local JSON API.
type Node struct {
	cfg    Config
	params *consensus.Params
	chain  *chain.Chain
	pool   *mempool.Mempool
	net    *p2p.Network
	http   *http.Server
	log    *slog.Logger

	mu      sync.Mutex
	orphans map[consensus.Hash]*consensus.Block
	waiting map[consensus.Hash][]consensus.Hash
	order   []consensus.Hash

	ctx     context.Context
	cancel  context.CancelFunc
	mineTo  consensus.Hash
	threads int
}

// Start opens the chain database, listens for peers, and serves the JSON API.
func Start(parent context.Context, cfg Config, log *slog.Logger) (*Node, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	var netw consensus.Network
	switch cfg.Network {
	case "mainnet", "":
		netw = consensus.Mainnet
		cfg.Network = "mainnet"
	case "devnet":
		netw = consensus.Devnet
	default:
		return nil, fmt.Errorf("unknown network %q", cfg.Network)
	}
	params := consensus.ByNetwork(netw)
	if cfg.DataDir == "" {
		return nil, errors.New("data directory is required")
	}
	if cfg.Listen == "" {
		cfg.Listen = DefaultP2PListen(netw)
	}
	if cfg.RPC == "" {
		cfg.RPC = DefaultRPCListen(netw)
	}
	peers, dnsErrs, err := p2p.ResolveSeeds(parent, cfg.Seeds, cfg.DNSSeeds, params.P2PPort, cfg.Lookup)
	if err != nil {
		return nil, err
	}
	for _, derr := range dnsErrs {
		log.Warn("dns seed", "err", derr)
	}
	if len(peers) > 0 {
		log.Info("dialing seeds", "peers", peers)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	ch, err := chain.Open(filepath.Join(cfg.DataDir, "chain.db"), params)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	n := &Node{
		cfg:     cfg,
		params:  params,
		chain:   ch,
		pool:    mempool.New(params, ch),
		log:     log,
		orphans: make(map[consensus.Hash]*consensus.Block),
		waiting: make(map[consensus.Hash][]consensus.Hash),
		ctx:     ctx,
		cancel:  cancel,
		threads: cfg.Threads,
	}
	if n.threads < 1 {
		n.threads = 1
	}
	if cfg.Mine {
		if cfg.MineTo == "" {
			n.closePartial()
			return nil, errors.New("--mine requires --mine-to")
		}
		hrp, pkh, err := address.Decode(cfg.MineTo)
		if err != nil {
			n.closePartial()
			return nil, err
		}
		if hrp != params.HRP {
			n.closePartial()
			return nil, fmt.Errorf("mine-to address is for %q, this node is %s", hrp, params.Name)
		}
		n.mineTo = pkh
	}
	pn, err := p2p.Start(ctx, p2p.Config{
		Magic:    params.Magic,
		Listen:   cfg.Listen,
		External: cfg.External,
		Pinned:   mergePeers(cfg.Connect, peers),
	}, n, log)
	if err != nil {
		n.closePartial()
		return nil, err
	}
	n.net = pn
	if cfg.RPC != "" {
		srv := &http.Server{
			Addr:        cfg.RPC,
			Handler:     n.routes(),
			ReadTimeout: 30 * time.Second,
			IdleTimeout: 2 * time.Minute,
		}
		ln, err := net.Listen("tcp", cfg.RPC)
		if err != nil {
			n.closePartial()
			return nil, err
		}
		n.cfg.RPC = ln.Addr().String()
		n.http = srv
		go func() {
			err := srv.Serve(ln)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				n.log.Error("rpc", "err", err)
			}
		}()
		if !localAPI(n.cfg.RPC) {
			n.log.Warn("JSON API has no authentication; bind it to localhost", "rpc", n.cfg.RPC)
		}
	}
	if cfg.Mine {
		go n.mineLoop()
	}
	n.log.Info("node started", "network", params.Name, "height", n.ChainHeight(), "p2p", pn.Addr(), "rpc", n.cfg.RPC)
	return n, nil
}

func (n *Node) closePartial() {
	n.cancel()
	if n.net != nil {
		_ = n.net.Close()
	}
	if n.chain != nil {
		_ = n.chain.Close()
	}
}

// Close stops mining, the API, the peer network, and the database.
func (n *Node) Close() error {
	n.cancel()
	if n.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = n.http.Shutdown(ctx)
		cancel()
	}
	var err error
	if n.net != nil {
		err = n.net.Close()
	}
	if n.chain != nil {
		if cerr := n.chain.Close(); err == nil {
			err = cerr
		}
	}
	return err
}

// P2PAddr is the bound peer address.
func (n *Node) P2PAddr() string {
	if n.net == nil {
		return ""
	}
	return n.net.Addr()
}

// RPCAddr is the configured API address.
func (n *Node) RPCAddr() string { return n.cfg.RPC }

// Params is the network this node is on.
func (n *Node) Params() *consensus.Params { return n.params }

// ChainHeight implements p2p.Handler.
func (n *Node) ChainHeight() uint64 {
	_, h := n.chain.Tip()
	return h
}

// Genesis implements p2p.Handler.
func (n *Node) Genesis() consensus.Hash { return n.chain.GenesisHash() }

// HasBlock implements p2p.Handler.
func (n *Node) HasBlock(h consensus.Hash) bool { return n.chain.HasBlock(h) }

// Locator implements p2p.Handler.
func (n *Node) Locator() []consensus.Hash { return n.chain.Locator() }

// BlocksAfter implements p2p.Handler.
func (n *Node) BlocksAfter(loc []consensus.Hash, stop consensus.Hash, limit int) []consensus.Hash {
	return n.chain.Locate(loc, stop, limit)
}

// Block implements p2p.Handler.
func (n *Node) Block(h consensus.Hash) (*consensus.Block, bool) {
	b, err := n.chain.Block(h)
	if err != nil {
		return nil, false
	}
	return b, true
}

// Tx implements p2p.Handler.
func (n *Node) Tx(id consensus.Hash) (*consensus.Tx, bool) {
	if tx, ok := n.pool.Get(id); ok {
		return tx, true
	}
	loc, ok, err := n.chain.FindTx(id)
	if err != nil || !ok {
		return nil, false
	}
	b, err := n.chain.Block(loc.Block)
	if err != nil || int(loc.Index) >= len(b.Txs) {
		return nil, false
	}
	return b.Txs[loc.Index], true
}

// NoteAddrs implements p2p.Handler. The peer manager stores the addresses.
func (n *Node) NoteAddrs(addrs []string) {
	if len(addrs) > 0 {
		n.log.Debug("addr", "n", len(addrs))
	}
}

// HandleBlock implements p2p.Handler.
func (n *Node) HandleBlock(block *consensus.Block) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	_, err := n.submitLocked(block)
	return err
}

// HandleTx implements p2p.Handler. A transaction that fails a consensus rule
// is dropped without disconnecting the peer.
func (n *Node) HandleTx(tx *consensus.Tx) error {
	return n.acceptTx(tx, false)
}

func (n *Node) acceptTx(tx *consensus.Tx, strict bool) error {
	err := n.pool.Add(tx)
	if err == nil {
		n.net.RelayTx(tx)
		return nil
	}
	if errors.Is(err, mempool.ErrKnown) {
		return nil
	}
	if !strict {
		if _, ok := consensus.AsRule(err); ok {
			return nil
		}
	}
	return err
}

// SubmitTx validates a transaction, puts it in the mempool, and relays it.
func (n *Node) SubmitTx(tx *consensus.Tx) error {
	return n.acceptTx(tx, true)
}

// SubmitBlock validates a block and relays it when it is new.
func (n *Node) SubmitBlock(block *consensus.Block) (chain.Accept, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.submitLocked(block)
}

func (n *Node) submitLocked(block *consensus.Block) (chain.Accept, error) {
	acc, err := n.chain.Process(block)
	if errors.Is(err, chain.ErrOrphan) {
		n.keepOrphan(block)
		n.net.RequestBlock(block.Header.Prev)
		return chain.Accept{}, nil
	}
	if err != nil {
		return chain.Accept{}, err
	}
	if acc.AlreadyHad {
		return acc, nil
	}
	n.absorb(block, acc)
	n.promote(block.Hash())
	return acc, nil
}

func (n *Node) keepOrphan(block *consensus.Block) {
	hash := block.Hash()
	if _, ok := n.orphans[hash]; ok {
		return
	}
	for len(n.orphans) >= maxOrphans && len(n.order) > 0 {
		old := n.order[0]
		n.order = n.order[1:]
		delete(n.orphans, old)
	}
	n.orphans[hash] = block
	n.order = append(n.order, hash)
	prev := block.Header.Prev
	n.waiting[prev] = append(n.waiting[prev], hash)
}

func (n *Node) promote(parent consensus.Hash) {
	kids := n.waiting[parent]
	delete(n.waiting, parent)
	for _, id := range kids {
		block := n.orphans[id]
		delete(n.orphans, id)
		if block == nil {
			continue
		}
		if _, err := n.submitLocked(block); err != nil {
			n.log.Info("orphan rejected", "hash", id, "err", err)
		}
	}
}

func (n *Node) absorb(block *consensus.Block, acc chain.Accept) {
	if acc.TipChanged {
		back := n.pool.OnReorg(acc)
		for _, tx := range back {
			if err := n.pool.Add(tx); err == nil {
				n.net.RelayTx(tx)
			}
		}
		for _, b := range acc.Added {
			n.net.RelayBlock(b)
		}
		n.log.Info("tip", "height", acc.Height, "hash", acc.Tip, "removed", len(acc.Removed), "added", len(acc.Added))
		return
	}
	n.net.RelayBlock(block)
}

// Generate mines blocks on the tip and pays the coinbase to payTo.
func (n *Node) Generate(ctx context.Context, payTo consensus.Hash, blocks int) error {
	if blocks < 1 || blocks > maxGenerate {
		return fmt.Errorf("blocks must be from 1 to %d", maxGenerate)
	}
	for i := 0; i < blocks; i++ {
		var last error
		mined := false
		for try := 0; try < 8; try++ {
			block, err := n.template(payTo)
			if err != nil {
				return err
			}
			if err := miner.Solve(ctx, block, n.threads); err != nil {
				return err
			}
			n.mu.Lock()
			acc, err := n.submitLocked(block)
			n.mu.Unlock()
			if err != nil {
				last = err
				continue
			}
			if acc.Tip == block.Hash() {
				mined = true
				break
			}
			last = errors.New("mined block did not become the tip")
		}
		if !mined {
			if last == nil {
				last = errors.New("could not mine a block")
			}
			return last
		}
	}
	return nil
}

func (n *Node) template(payTo consensus.Hash) (*consensus.Block, error) {
	budget := consensus.MaxBlockBytes - templateRoom
	txs, fees := n.pool.Select(budget)
	return n.chain.Template(payTo, 0, txs, fees)
}

func (n *Node) mineLoop() {
	for {
		if n.ctx.Err() != nil {
			return
		}
		err := n.Generate(n.ctx, n.mineTo, 1)
		if n.ctx.Err() != nil {
			return
		}
		if err != nil {
			n.log.Error("miner", "err", err)
			select {
			case <-n.ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

// PauseDial stops outbound redials. Tests use it to partition the network.
func (n *Node) PauseDial(pause bool) { n.net.PauseDial(pause) }

// DisconnectAll closes current peer connections.
func (n *Node) DisconnectAll() { n.net.DisconnectAll() }

// Connect dials a peer and completes the handshake.
func (n *Node) Connect(ctx context.Context, addr string) error { return n.net.Connect(ctx, addr) }

// PeerCount is the number of completed handshakes.
func (n *Node) PeerCount() int { return n.net.PeerCount() }

// UTXO is one output known to the API.
type UTXO struct {
	TxID     string `json:"txid"`
	Index    uint32 `json:"index"`
	Atoms    string `json:"atoms"`
	Height   uint64 `json:"height"`
	Coinbase bool   `json:"coinbase"`
	Mature   bool   `json:"mature"`
	Reserved bool   `json:"reserved"`
}

// AddressInfo is the confirmed state of one address.
type AddressInfo struct {
	Address        string `json:"address"`
	Confirmed      string `json:"confirmed"`
	ConfirmedAtoms string `json:"confirmed_atoms"`
	Spendable      string `json:"spendable"`
	SpendableAtoms string `json:"spendable_atoms"`
	UTXOs          []UTXO `json:"utxos"`
}

// LookupAddress returns confirmed outputs for a bech32m address on this network.
func (n *Node) LookupAddress(addr string) (AddressInfo, error) {
	hrp, pkh, err := address.Decode(addr)
	if err != nil {
		return AddressInfo{}, err
	}
	if hrp != n.params.HRP {
		return AddressInfo{}, fmt.Errorf("address is for %q, this node is %s", hrp, n.params.Name)
	}
	rows, err := n.chain.UTXOsByAddress(pkh)
	if err != nil {
		return AddressInfo{}, err
	}
	_, tip := n.chain.Tip()
	spendHeight := tip + 1
	info := AddressInfo{Address: addr, UTXOs: []UTXO{}}
	var confirmed, spendable uint64
	for _, row := range rows {
		mature := !row.UTXO.Coinbase || (spendHeight >= row.UTXO.Height && spendHeight-row.UTXO.Height >= consensus.CoinbaseMaturity)
		reserved := n.pool.Spends(row.Outpoint)
		info.UTXOs = append(info.UTXOs, UTXO{
			TxID:     row.Outpoint.Hash.String(),
			Index:    row.Outpoint.Index,
			Atoms:    fmt.Sprintf("%d", row.UTXO.Value),
			Height:   row.UTXO.Height,
			Coinbase: row.UTXO.Coinbase,
			Mature:   mature,
			Reserved: reserved,
		})
		confirmed += row.UTXO.Value
		if mature && !reserved {
			spendable += row.UTXO.Value
		}
	}
	info.Confirmed = amount.Format(confirmed)
	info.ConfirmedAtoms = fmt.Sprintf("%d", confirmed)
	info.Spendable = amount.Format(spendable)
	info.SpendableAtoms = fmt.Sprintf("%d", spendable)
	return info, nil
}

// Tip is the active chain tip.
func (n *Node) Tip() (consensus.Hash, uint64) { return n.chain.Tip() }

// MempoolLen is the number of transactions waiting for a block.
func (n *Node) MempoolLen() int { return n.pool.Len() }

// Software version served by the API.
func Software() string { return version.Software }

// Protocol version served by the API.
func Protocol() uint32 { return version.Protocol }
