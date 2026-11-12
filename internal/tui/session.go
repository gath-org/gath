package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gath-org/gath/internal/client"
	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/node"
	"github.com/gath-org/gath/internal/spend"
	"github.com/gath-org/gath/internal/wallet"
)

// Options selects where a terminal session keeps its node and wallet.
// Empty Listen and RPC use the network's published addresses.
type Options struct {
	Network string
	Home    string
	Listen  string
	RPC     string
	Threads int
}

// Snapshot is one read of the node, the wallet, and the miner.
type Snapshot struct {
	Network  string
	Owned    bool
	Attached bool
	RPC      string
	P2P      string
	DataDir  string
	NodeErr  string
	Status   client.Status
	Peers    []client.Peer
	Miner    MinerStatus
	Wallet   WalletSnap
}

// WalletSnap is the wallet half of a snapshot.
type WalletSnap struct {
	Exists   bool
	Unlocked bool
	Address  string
	Balance  client.Address
	History  []client.HistoryTx
	Err      string
}

// Session is the node, wallet, and miner behind the terminal.
type Session struct {
	mu      sync.Mutex
	network string
	home    string
	listen  string
	rpc     string
	dataDir string
	client  *client.Client
	owned   bool
	node    *node.Node
	logFile *os.File
	rpcAddr string
	p2pAddr string
	last    client.Status
	wallet  *wallet.Wallet
	addr    string
	balance client.Address
	history []client.HistoryTx
	miner   *Miner
}

var _ host = (*Session)(nil)

// NewSession builds a session. It does not listen until Open.
func NewSession(opt Options) *Session {
	network := opt.Network
	if network == "" {
		network = DefaultNetwork
	}
	threads := opt.Threads
	if threads == 0 {
		threads = runtime.NumCPU()
	}
	return &Session{
		network: network,
		home:    opt.Home,
		listen:  opt.Listen,
		rpc:     opt.RPC,
		miner:   NewMiner(threads),
	}
}

// Network is the session's current network.
func (s *Session) Network() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.network == "" {
		return DefaultNetwork
	}
	return s.network
}

// Open attaches to a node already listening on this network, or starts one.
func (s *Session) Open(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.snapshotLocked(), nil
	}
	err := s.openLocked(ctx)
	snap := s.snapshotLocked()
	if err != nil {
		snap.NodeErr = err.Error()
	}
	return snap, err
}

// Reopen drops the current API connection and opens the network again.
// A node this session started is stopped first. An attached node is left running.
func (s *Session) Reopen(ctx context.Context) (Snapshot, error) {
	s.miner.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeNodeLocked()
	err := s.openLocked(ctx)
	snap := s.snapshotLocked()
	if err != nil {
		snap.NodeErr = err.Error()
	}
	return snap, err
}

// Poll reads the node and, when the wallet is unlocked, its balance and history.
func (s *Session) Poll(ctx context.Context) Snapshot {
	s.mu.Lock()
	c := s.client
	addr := s.addr
	network := s.network
	snap := s.snapshotLocked()
	s.mu.Unlock()
	snap.Miner = s.miner.Status()
	if c == nil {
		if snap.NodeErr == "" {
			snap.NodeErr = errNoNode.Error()
		}
		return snap
	}
	st, err := c.Status(ctx)
	if err != nil {
		snap.NodeErr = err.Error()
		return snap
	}
	if st.Network != network {
		snap.NodeErr = fmt.Sprintf("the API is %s", st.Network)
		return snap
	}
	snap.Status = st
	snap.NodeErr = ""
	if peers, err := c.Peers(ctx); err == nil {
		snap.Peers = peers
	}
	s.mu.Lock()
	if s.network == network {
		s.last = st
	}
	s.mu.Unlock()
	if addr == "" {
		return snap
	}
	info, err := c.Address(ctx, addr)
	if err != nil {
		snap.Wallet.Err = err.Error()
	} else {
		snap.Wallet.Balance = info
		s.mu.Lock()
		if s.addr == addr {
			s.balance = info
		}
		s.mu.Unlock()
	}
	hist, err := c.History(ctx, addr)
	if err != nil {
		if snap.Wallet.Err == "" {
			snap.Wallet.Err = err.Error()
		}
	} else {
		snap.Wallet.History = hist.Transactions
		s.mu.Lock()
		if s.addr == addr {
			s.history = hist.Transactions
		}
		s.mu.Unlock()
	}
	return snap
}

// Switch stops the miner, drops the unlocked wallet, and opens network.
func (s *Session) Switch(ctx context.Context, network string) (Snapshot, error) {
	if err := ValidateNetwork(network); err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	same := network == s.network && s.client != nil
	s.mu.Unlock()
	if same {
		return s.Poll(ctx), nil
	}
	s.miner.Reset()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeNodeLocked()
	s.clearWalletLocked()
	s.network = network
	err := s.openLocked(ctx)
	snap := s.snapshotLocked()
	if err != nil {
		snap.NodeErr = err.Error()
		snap.Network = network
	}
	return snap, err
}

// Close stops the miner and a node this session started.
// A node it only attached to keeps running.
func (s *Session) Close() error {
	s.miner.Reset()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeNodeLocked()
	s.clearWalletLocked()
	return nil
}

// SetThreads changes the miner worker count.
func (s *Session) SetThreads(n int) MinerStatus {
	return s.miner.SetThreads(n)
}

// StartMiner hashes for the unlocked wallet on this network.
func (s *Session) StartMiner() (MinerStatus, error) {
	s.mu.Lock()
	api := s.client
	addr := s.addr
	s.mu.Unlock()
	if err := s.miner.Start(api, addr); err != nil {
		return s.miner.Status(), err
	}
	return s.miner.Status(), nil
}

// StopMiner stops hashing and keeps the counters from this run.
func (s *Session) StopMiner() MinerStatus {
	return s.miner.Stop()
}

// CreateWallet writes a new wallet for the current network and unlocks it.
// The returned phrase is the only backup and is not stored in the file.
func (s *Session) CreateWallet(pass string) (string, string, error) {
	network, home, path, err := s.walletPath()
	if err != nil {
		return "", "", err
	}
	if fileExists(path) {
		return "", "", errors.New("a wallet file already exists for this network")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", err
	}
	phrase, w, err := wallet.Create(path, network, pass)
	if err != nil {
		return "", "", err
	}
	addr, err := w.Address()
	if err != nil {
		return "", "", err
	}
	if err := s.holdWallet(network, home, w, addr); err != nil {
		return phrase, addr, err
	}
	return phrase, addr, nil
}

// RestoreWallet writes a wallet from a recovery phrase and unlocks it.
func (s *Session) RestoreWallet(phrase, pass string) (string, error) {
	network, home, path, err := s.walletPath()
	if err != nil {
		return "", err
	}
	if fileExists(path) {
		return "", errors.New("a wallet file already exists for this network")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	w, err := wallet.Restore(path, network, cleanPhrase(phrase), pass)
	if err != nil {
		return "", err
	}
	addr, err := w.Address()
	if err != nil {
		return "", err
	}
	if err := s.holdWallet(network, home, w, addr); err != nil {
		return addr, err
	}
	return addr, nil
}

// UnlockWallet decrypts the wallet file for the current network.
func (s *Session) UnlockWallet(pass string) (string, error) {
	network, home, path, err := s.walletPath()
	if err != nil {
		return "", err
	}
	if !fileExists(path) {
		return "", errors.New("no wallet file for this network")
	}
	w, err := wallet.Load(path, pass)
	if err != nil {
		return "", err
	}
	if w.Network() != network {
		return "", fmt.Errorf("this wallet file is %s", w.Network())
	}
	addr, err := w.Address()
	if err != nil {
		return "", err
	}
	if err := s.holdWallet(network, home, w, addr); err != nil {
		return "", err
	}
	return addr, nil
}

// LockWallet forgets the unlocked key. The file stays on disk.
func (s *Session) LockWallet() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearWalletLocked()
}

// Send pays amount Gath from the unlocked wallet.
func (s *Session) Send(ctx context.Context, to, amount string) (string, error) {
	s.mu.Lock()
	w := s.wallet
	c := s.client
	s.mu.Unlock()
	if w == nil {
		return "", errNoWallet
	}
	if c == nil {
		return "", errNoNode
	}
	return spend.Send(ctx, w, c, to, amount)
}

func (s *Session) walletPath() (network, home, path string, err error) {
	s.mu.Lock()
	network = s.network
	home = s.home
	s.mu.Unlock()
	if network == "" {
		network = DefaultNetwork
	}
	if home == "" {
		home, err = DefaultHome()
		if err != nil {
			return "", "", "", err
		}
		s.mu.Lock()
		if s.home == "" {
			s.home = home
		} else {
			home = s.home
		}
		s.mu.Unlock()
	}
	_, path, err = Layout(home, network)
	return network, home, path, err
}

func (s *Session) holdWallet(network, home string, w *wallet.Wallet, addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.network != network {
		return fmt.Errorf("network changed; the wallet was saved for %s", network)
	}
	if home != "" && s.home == "" {
		s.home = home
	}
	s.wallet = w
	s.addr = addr
	s.balance = client.Address{}
	s.history = nil
	return nil
}

func (s *Session) openLocked(ctx context.Context) error {
	if s.network == "" {
		s.network = DefaultNetwork
	}
	if err := ValidateNetwork(s.network); err != nil {
		return err
	}
	if s.home == "" {
		home, err := DefaultHome()
		if err != nil {
			return err
		}
		s.home = home
	}
	dataDir, walletPath, err := Layout(s.home, s.network)
	if err != nil {
		return err
	}
	s.dataDir = dataDir
	net, err := consensus.ParseNetwork(s.network)
	if err != nil {
		return err
	}
	rpc := s.rpc
	if rpc == "" {
		rpc = node.DefaultRPCListen(net)
	}
	if !ephemeral(rpc) {
		probe, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
		c := client.New("http://" + rpc)
		st, probeErr := c.Status(probe)
		cancel()
		if probeErr == nil {
			if st.Network != s.network {
				return fmt.Errorf("the API at %s is %s", rpc, st.Network)
			}
			s.client = c
			s.owned = false
			s.rpcAddr = rpc
			s.p2pAddr = ""
			s.last = st
			_ = walletPath
			return nil
		}
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	logger, file, err := openLog(dataDir)
	if err != nil {
		return err
	}
	cfg, err := NodeConfig(s.network, dataDir, s.listen, s.rpc)
	if err != nil {
		if file != nil {
			_ = file.Close()
		}
		return err
	}
	nd, err := node.Start(context.Background(), cfg, logger)
	if err != nil {
		if file != nil {
			_ = file.Close()
		}
		return err
	}
	s.node = nd
	s.owned = true
	s.logFile = file
	s.rpcAddr = nd.RPCAddr()
	s.p2pAddr = nd.P2PAddr()
	s.client = client.New("http://" + nd.RPCAddr())
	st, err := s.client.Status(ctx)
	if err != nil {
		s.closeNodeLocked()
		return err
	}
	if st.Network != s.network {
		s.closeNodeLocked()
		return fmt.Errorf("started node is %s", st.Network)
	}
	s.last = st
	return nil
}

func (s *Session) snapshotLocked() Snapshot {
	_, walletPath, _ := Layout(s.home, s.network)
	snap := Snapshot{
		Network:  s.network,
		Owned:    s.owned && s.node != nil,
		Attached: s.client != nil && !s.owned,
		RPC:      s.rpcAddr,
		P2P:      s.p2pAddr,
		DataDir:  s.dataDir,
		Status:   s.last,
		Miner:    s.miner.Status(),
		Wallet: WalletSnap{
			Exists:   walletPath != "" && fileExists(walletPath),
			Unlocked: s.wallet != nil,
			Address:  s.addr,
			Balance:  s.balance,
			History:  append([]client.HistoryTx(nil), s.history...),
		},
	}
	if snap.Network == "" {
		snap.Network = DefaultNetwork
	}
	return snap
}

func (s *Session) closeNodeLocked() {
	if s.node != nil {
		_ = s.node.Close()
		s.node = nil
	}
	if s.logFile != nil {
		_ = s.logFile.Close()
		s.logFile = nil
	}
	s.client = nil
	s.owned = false
	s.rpcAddr = ""
	s.p2pAddr = ""
}

func (s *Session) clearWalletLocked() {
	s.wallet = nil
	s.addr = ""
	s.balance = client.Address{}
	s.history = nil
}

func ephemeral(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	return err == nil && port == "0"
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func openLog(dir string) (*slog.Logger, *os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "node.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil
	}
	return slog.New(slog.NewTextHandler(f, nil)), f, nil
}
