package p2p

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/version"
)

// Handler is the chain-facing side of the peer network.
type Handler interface {
	ChainHeight() uint64
	Genesis() consensus.Hash
	HasBlock(consensus.Hash) bool
	Locator() []consensus.Hash
	HandleBlock(*consensus.Block) error
	HandleTx(*consensus.Tx) error
	BlocksAfter([]consensus.Hash, consensus.Hash, int) []consensus.Hash
	Block(consensus.Hash) (*consensus.Block, bool)
	Tx(consensus.Hash) (*consensus.Tx, bool)
	NoteAddrs([]string)
}

// Config is the listen address and the peers we always try to reach.
type Config struct {
	Magic    uint32
	Listen   string
	External string
	Pinned   []string
}

// Network is the node's peer manager.
type Network struct {
	cfg     Config
	handler Handler
	log     *slog.Logger
	nonce   uint64

	ctx    context.Context
	cancel context.CancelFunc
	ln     net.Listener

	mu       sync.Mutex
	peers    map[uint64]*peer
	byAddr   map[string]*peer
	known    map[string]struct{}
	paused   bool
	selfAddr string
}

type peer struct {
	nonce    uint64
	addr     string
	dialed   string
	outbound bool
	conn     net.Conn
	out      chan []byte
	height   uint64
	agent    string
	net      *Network
	done     chan struct{}
}

// Start listens and dials pinned peers.
func Start(parent context.Context, cfg Config, h Handler, log *slog.Logger) (*Network, error) {
	if log == nil {
		log = slog.Default()
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, err
	}
	var nonce uint64
	if err := binary.Read(rand.Reader, binary.LittleEndian, &nonce); err != nil {
		_ = ln.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	n := &Network{
		cfg:      cfg,
		handler:  h,
		log:      log,
		nonce:    nonce,
		ctx:      ctx,
		cancel:   cancel,
		ln:       ln,
		peers:    make(map[uint64]*peer),
		byAddr:   make(map[string]*peer),
		known:    make(map[string]struct{}),
		selfAddr: advertisedAddr(cfg.External, ln.Addr().String()),
	}
	go n.accept()
	go n.maintain()
	return n, nil
}

// advertisedAddr is the address we tell peers to dial. An explicit external
// address is used as given. A loopback listen address is advertised so local
// peers can reconnect. An unspecified bind (0.0.0.0 or ::) is not advertised:
// it is not a destination other nodes can dial.
func advertisedAddr(external, bound string) string {
	if external != "" {
		return external
	}
	host, _, err := net.SplitHostPort(bound)
	if err != nil || host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return ""
	}
	return bound
}

const maxKnownAddrs = 1024

// routableAddr is an address we are willing to dial because another peer
// told us about it. Pinned peers and --connect are not filtered here.
// Hostnames are rejected: a peer could point them at an internal name.
func routableAddr(addr string) bool {
	if err := validatePeerAddr(addr); err != nil {
		return false
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate()
}

// addKnownLocked records a discovery address. Caller holds n.mu.
func (n *Network) addKnownLocked(addr string) {
	if addr == "" || addr == n.selfAddr {
		return
	}
	if _, ok := n.known[addr]; ok {
		return
	}
	if len(n.known) >= maxKnownAddrs {
		return
	}
	n.known[addr] = struct{}{}
}

// Addr is the address the listener is bound to.
func (n *Network) Addr() string { return n.ln.Addr().String() }

// External is the address we advertise to peers.
func (n *Network) External() string { return n.selfAddr }

// Close shuts down the listener and every peer.
func (n *Network) Close() error {
	n.cancel()
	err := n.ln.Close()
	n.mu.Lock()
	peers := make([]*peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.mu.Unlock()
	for _, p := range peers {
		_ = p.conn.Close()
	}
	return err
}

// PauseDial stops or resumes outbound discovery and pinned redials.
func (n *Network) PauseDial(pause bool) {
	n.mu.Lock()
	n.paused = pause
	n.mu.Unlock()
}

// DisconnectAll closes every current connection.
func (n *Network) DisconnectAll() {
	n.mu.Lock()
	peers := make([]*peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.mu.Unlock()
	for _, p := range peers {
		_ = p.conn.Close()
	}
}

// Connect dials addr and waits for the handshake.
func (n *Network) Connect(ctx context.Context, addr string) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return n.handshake(conn, addr, true)
}

// PeerCount is the number of completed handshakes.
func (n *Network) PeerCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.peers)
}

// Peer is a connected peer as shown by the API.
type Peer struct {
	Addr     string `json:"addr"`
	Agent    string `json:"agent"`
	Height   uint64 `json:"height"`
	Outbound bool   `json:"outbound"`
}

// Peers lists completed handshakes.
func (n *Network) Peers() []Peer {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]Peer, 0, len(n.peers))
	for _, p := range n.peers {
		out = append(out, Peer{Addr: p.addr, Agent: p.agent, Height: p.height, Outbound: p.outbound})
	}
	return out
}

// RelayBlock announces a block and pushes the body.
func (n *Network) RelayBlock(block *consensus.Block) {
	raw := block.Bytes()
	inv, err := encodeInv([]invItem{{kind: invBlock, hash: block.Hash()}})
	if err != nil {
		return
	}
	n.broadcast(writeFrame(n.cfg.Magic, cmdInv, inv), writeFrame(n.cfg.Magic, cmdBlock, raw))
}

// RelayTx announces a transaction and pushes the body.
func (n *Network) RelayTx(tx *consensus.Tx) {
	inv, err := encodeInv([]invItem{{kind: invTx, hash: tx.ID()}})
	if err != nil {
		return
	}
	n.broadcast(writeFrame(n.cfg.Magic, cmdInv, inv), writeFrame(n.cfg.Magic, cmdTx, tx.Bytes()))
}

// RequestBlock asks peers for a block by hash.
func (n *Network) RequestBlock(hash consensus.Hash) {
	payload, err := encodeInv([]invItem{{kind: invBlock, hash: hash}})
	if err != nil {
		return
	}
	n.broadcast(writeFrame(n.cfg.Magic, cmdGetData, payload))
}

func (n *Network) broadcast(frames ...[]byte) {
	n.mu.Lock()
	peers := make([]*peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.mu.Unlock()
	for _, p := range peers {
		for _, f := range frames {
			p.send(f)
		}
	}
}

func (n *Network) accept() {
	for {
		conn, err := n.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			if err := n.handshake(conn, conn.RemoteAddr().String(), false); err != nil {
				_ = conn.Close()
			}
		}()
	}
}

func (n *Network) maintain() {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	n.dialPinned()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-tick.C:
			n.mu.Lock()
			paused := n.paused
			n.mu.Unlock()
			if paused {
				continue
			}
			n.dialPinned()
			n.dialKnown()
		}
	}
}

func (n *Network) dialPinned() {
	for _, addr := range n.cfg.Pinned {
		n.mu.Lock()
		_, exists := n.byAddr[addr]
		paused := n.paused
		n.mu.Unlock()
		if exists || paused {
			continue
		}
		go func(addr string) {
			ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
			defer cancel()
			_ = n.Connect(ctx, addr)
		}(addr)
	}
}

func (n *Network) dialKnown() {
	n.mu.Lock()
	if len(n.peers) >= 8 {
		n.mu.Unlock()
		return
	}
	var todo []string
	for addr := range n.known {
		if _, ok := n.byAddr[addr]; ok || addr == n.selfAddr || !routableAddr(addr) {
			continue
		}
		todo = append(todo, addr)
		if len(todo) >= 8 {
			break
		}
	}
	n.mu.Unlock()
	for _, addr := range todo {
		go func(addr string) {
			ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
			defer cancel()
			_ = n.Connect(ctx, addr)
		}(addr)
	}
}

func (n *Network) handshake(conn net.Conn, addr string, outbound bool) error {
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	local := versionMsg{
		Version:   version.Protocol,
		Services:  1,
		Timestamp: uint64(time.Now().Unix()),
		Nonce:     n.nonce,
		Height:    n.handler.ChainHeight(),
		Genesis:   n.handler.Genesis(),
		Agent:     version.UserAgent,
		Listen:    n.selfAddr,
	}
	if _, err := conn.Write(writeFrame(n.cfg.Magic, cmdVersion, encodeVersion(local))); err != nil {
		return err
	}
	var remote versionMsg
	gotVersion := false
	gotVerack := false
	for !gotVersion || !gotVerack {
		cmd, payload, err := readFrame(conn, n.cfg.Magic)
		if err != nil {
			return err
		}
		switch cmd {
		case cmdVersion:
			msg, err := decodeVersion(payload)
			if err != nil {
				return err
			}
			if msg.Genesis != n.handler.Genesis() {
				return errors.New("genesis mismatch")
			}
			if msg.Nonce == n.nonce {
				return errors.New("connected to self")
			}
			remote = msg
			gotVersion = true
			if _, err := conn.Write(writeFrame(n.cfg.Magic, cmdVerack, nil)); err != nil {
				return err
			}
		case cmdVerack:
			gotVerack = true
		default:
			// Ignore anything else until the handshake finishes.
		}
	}
	p := &peer{
		nonce:    remote.Nonce,
		addr:     addr,
		outbound: outbound,
		conn:     conn,
		out:      make(chan []byte, 64),
		height:   remote.Height,
		agent:    remote.Agent,
		net:      n,
		done:     make(chan struct{}),
	}
	var listenAddr string
	if remote.Listen != "" && validatePeerAddr(remote.Listen) == nil {
		listenAddr = remote.Listen
		p.addr = listenAddr
	}
	if outbound {
		p.dialed = addr
	}
	n.mu.Lock()
	if _, exists := n.peers[p.nonce]; exists {
		n.mu.Unlock()
		return errors.New("duplicate peer")
	}
	n.peers[p.nonce] = p
	// byAddr stops a redial of an address we already have. known is only
	// addresses other nodes can dial: a public IP and port, not an inbound
	// source port, loopback, or a private network.
	if listenAddr != "" && routableAddr(listenAddr) {
		n.byAddr[listenAddr] = p
		n.addKnownLocked(listenAddr)
	}
	if p.dialed != "" {
		n.byAddr[p.dialed] = p
		if routableAddr(p.dialed) {
			n.addKnownLocked(p.dialed)
		}
	}
	n.mu.Unlock()
	n.log.Info("peer connected", "addr", p.addr, "height", p.height, "outbound", outbound)
	go p.writeLoop()
	if n.selfAddr != "" {
		payload, err := encodeAddr(uint64(time.Now().Unix()), []string{n.selfAddr})
		if err == nil {
			p.send(writeFrame(n.cfg.Magic, cmdAddr, payload))
		}
	}
	p.send(writeFrame(n.cfg.Magic, cmdGetAddr, nil))
	p.sendGetBlocks()
	_ = conn.SetDeadline(time.Time{})
	go p.writeLoop()
	go p.readLoop()
	return nil
}

func (p *peer) send(frame []byte) {
	select {
	case <-p.done:
		return
	case p.out <- frame:
	default:
		_ = p.conn.Close()
	}
}

func (p *peer) writeLoop() {
	ping := time.NewTicker(45 * time.Second)
	defer ping.Stop()
	var nonce uint64
	for {
		select {
		case <-p.done:
			return
		case <-p.net.ctx.Done():
			return
		case frame := <-p.out:
			_ = p.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := p.conn.Write(frame); err != nil {
				_ = p.conn.Close()
				return
			}
		case <-ping.C:
			nonce++
			frame := writeFrame(p.net.cfg.Magic, cmdPing, encodePing(nonce))
			_ = p.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := p.conn.Write(frame); err != nil {
				_ = p.conn.Close()
				return
			}
		}
	}
}

func (p *peer) readLoop() {
	defer p.close()
	for {
		_ = p.conn.SetReadDeadline(time.Now().Add(3 * time.Minute))
		cmd, payload, err := readFrame(p.conn, p.net.cfg.Magic)
		if err != nil {
			if !errors.Is(err, io.EOF) && p.net.ctx.Err() == nil {
				p.net.log.Debug("peer read", "addr", p.addr, "err", err)
			}
			return
		}
		if err := p.on(cmd, payload); err != nil {
			p.net.log.Info("peer dropped", "addr", p.addr, "err", err)
			return
		}
	}
}

func (p *peer) on(cmd string, payload []byte) error {
	switch cmd {
	case cmdPing:
		nonce, err := decodePing(payload)
		if err != nil {
			return err
		}
		p.send(writeFrame(p.net.cfg.Magic, cmdPong, encodePing(nonce)))
		return nil
	case cmdPong:
		return nil
	case cmdGetAddr:
		p.net.mu.Lock()
		addrs := make([]string, 0, len(p.net.known))
		for a := range p.net.known {
			addrs = append(addrs, a)
		}
		p.net.mu.Unlock()
		raw, err := encodeAddr(uint64(time.Now().Unix()), addrs)
		if err != nil {
			return err
		}
		p.send(writeFrame(p.net.cfg.Magic, cmdAddr, raw))
		return nil
	case cmdAddr:
		addrs, err := decodeAddr(payload)
		if err != nil {
			return err
		}
		var keep []string
		p.net.mu.Lock()
		for _, a := range addrs {
			if !routableAddr(a) {
				continue
			}
			keep = append(keep, a)
			p.net.addKnownLocked(a)
		}
		p.net.mu.Unlock()
		if len(keep) > 0 {
			p.net.handler.NoteAddrs(keep)
		}
		return nil
	case cmdGetBlocks:
		loc, stop, err := decodeGetBlocks(payload)
		if err != nil {
			return err
		}
		hashes := p.net.handler.BlocksAfter(loc, stop, 500)
		items := make([]invItem, len(hashes))
		for i, h := range hashes {
			items[i] = invItem{kind: invBlock, hash: h}
		}
		raw, err := encodeInv(items)
		if err != nil {
			return err
		}
		p.send(writeFrame(p.net.cfg.Magic, cmdInv, raw))
		return nil
	case cmdInv:
		items, err := decodeInv(payload)
		if err != nil {
			return err
		}
		var want []invItem
		for _, it := range items {
			switch it.kind {
			case invBlock:
				if !p.net.handler.HasBlock(it.hash) {
					want = append(want, it)
				}
			case invTx:
				if _, ok := p.net.handler.Tx(it.hash); !ok {
					want = append(want, it)
				}
			default:
				return errors.New("unknown inventory type")
			}
			if len(want) == 500 {
				break
			}
		}
		if len(want) == 0 {
			return nil
		}
		raw, err := encodeInv(want)
		if err != nil {
			return err
		}
		p.send(writeFrame(p.net.cfg.Magic, cmdGetData, raw))
		return nil
	case cmdGetData:
		items, err := decodeInv(payload)
		if err != nil {
			return err
		}
		for _, it := range items {
			switch it.kind {
			case invBlock:
				block, ok := p.net.handler.Block(it.hash)
				if ok {
					p.send(writeFrame(p.net.cfg.Magic, cmdBlock, block.Bytes()))
				}
			case invTx:
				tx, ok := p.net.handler.Tx(it.hash)
				if ok {
					p.send(writeFrame(p.net.cfg.Magic, cmdTx, tx.Bytes()))
				}
			default:
				return errors.New("unknown inventory type")
			}
		}
		return nil
	case cmdBlock:
		block, err := consensus.DecodeBlock(payload)
		if err != nil {
			return err
		}
		if err := p.net.handler.HandleBlock(block); err != nil {
			return err
		}
		if p.height > p.net.handler.ChainHeight() {
			p.sendGetBlocks()
		}
		return nil
	case cmdTx:
		tx, err := consensus.DecodeTx(payload)
		if err != nil {
			return err
		}
		return p.net.handler.HandleTx(tx)
	case cmdVersion, cmdVerack:
		return nil
	default:
		return errors.New("unknown command " + cmd)
	}
}

func (p *peer) sendGetBlocks() {
	loc := p.net.handler.Locator()
	if len(loc) == 0 {
		loc = []consensus.Hash{p.net.handler.Genesis()}
	}
	raw, err := encodeGetBlocks(loc, consensus.Hash{})
	if err != nil {
		return
	}
	p.send(writeFrame(p.net.cfg.Magic, cmdGetBlocks, raw))
}

func (p *peer) close() {
	p.net.mu.Lock()
	delete(p.net.peers, p.nonce)
	for addr, cur := range p.net.byAddr {
		if cur == p {
			delete(p.net.byAddr, addr)
		}
	}
	p.net.mu.Unlock()
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	_ = p.conn.Close()
	p.net.log.Info("peer disconnected", "addr", p.addr)
}

// AskBlocks sends getblocks with the supplied locator.
func (n *Network) AskBlocks(locators []consensus.Hash) {
	if len(locators) == 0 {
		return
	}
	raw, err := encodeGetBlocks(locators, consensus.Hash{})
	if err != nil {
		return
	}
	n.broadcast(writeFrame(n.cfg.Magic, cmdGetBlocks, raw))
}
