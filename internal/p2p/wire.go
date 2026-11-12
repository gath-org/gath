package p2p

import (
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/sha3"

	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/version"
)

const (
	invTx    uint32 = 1
	invBlock uint32 = 2

	maxInv       = 5000
	maxAddr      = 100
	maxLocators  = 64
	maxUserAgent = 256
	maxAddrLen   = 256
	maxPayload   = consensus.MaxBlockBytes + 1024
)

const (
	cmdVersion   = "version"
	cmdVerack    = "verack"
	cmdPing      = "ping"
	cmdPong      = "pong"
	cmdGetAddr   = "getaddr"
	cmdAddr      = "addr"
	cmdInv       = "inv"
	cmdGetData   = "getdata"
	cmdGetBlocks = "getblocks"
	cmdBlock     = "block"
	cmdTx        = "tx"
)

type versionMsg struct {
	Version   uint32
	Services  uint64
	Timestamp uint64
	Nonce     uint64
	Height    uint64
	Genesis   consensus.Hash
	Agent     string
	Listen    string
}

type invItem struct {
	kind uint32
	hash consensus.Hash
}

func writeFrame(magic uint32, command string, payload []byte) []byte {
	var cmd [12]byte
	copy(cmd[:], command)
	sum := sha3.Sum256(payload)
	buf := make([]byte, 4+12+4+4+len(payload))
	binary.LittleEndian.PutUint32(buf[0:], magic)
	copy(buf[4:], cmd[:])
	binary.LittleEndian.PutUint32(buf[16:], uint32(len(payload)))
	copy(buf[20:24], sum[:4])
	copy(buf[24:], payload)
	return buf
}

func readFrame(r io.Reader, magic uint32) (string, []byte, error) {
	var head [24]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return "", nil, err
	}
	got := binary.LittleEndian.Uint32(head[0:4])
	if got != magic {
		return "", nil, errors.New("wrong network magic")
	}
	cmd := string(trimRight(head[4:16]))
	n := binary.LittleEndian.Uint32(head[16:20])
	if n > maxPayload {
		return "", nil, errors.New("message too large")
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return "", nil, err
	}
	sum := sha3.Sum256(payload)
	if string(sum[:4]) != string(head[20:24]) {
		return "", nil, errors.New("bad checksum")
	}
	return cmd, payload, nil
}

func trimRight(b []byte) []byte {
	i := len(b)
	for i > 0 && b[i-1] == 0 {
		i--
	}
	return b[:i]
}

func encodeVersion(m versionMsg) []byte {
	w := consensus.W{}
	w.U32(m.Version)
	w.U64(m.Services)
	w.U64(m.Timestamp)
	w.U64(m.Nonce)
	w.U64(m.Height)
	w.Hash(m.Genesis)
	w.VarBytes([]byte(m.Agent))
	w.VarBytes([]byte(m.Listen))
	return w.Bytes()
}

func decodeVersion(b []byte) (versionMsg, error) {
	r := consensus.NewReader(b)
	var m versionMsg
	var err error
	if m.Version, err = r.U32(); err != nil {
		return m, err
	}
	if m.Services, err = r.U64(); err != nil {
		return m, err
	}
	if m.Timestamp, err = r.U64(); err != nil {
		return m, err
	}
	if m.Nonce, err = r.U64(); err != nil {
		return m, err
	}
	if m.Height, err = r.U64(); err != nil {
		return m, err
	}
	if m.Genesis, err = r.Hash(); err != nil {
		return m, err
	}
	agent, err := r.VarBytes(maxUserAgent)
	if err != nil {
		return m, err
	}
	listen, err := r.VarBytes(maxAddrLen)
	if err != nil {
		return m, err
	}
	if err := r.Empty(); err != nil {
		return m, err
	}
	m.Agent = string(agent)
	m.Listen = string(listen)
	if m.Version != version.Protocol {
		return m, errors.New("unsupported protocol version")
	}
	return m, nil
}

func encodeInv(items []invItem) ([]byte, error) {
	if len(items) > maxInv {
		return nil, errors.New("too many inventory items")
	}
	w := consensus.W{}
	w.VarInt(uint64(len(items)))
	for _, it := range items {
		w.U32(it.kind)
		w.Hash(it.hash)
	}
	return w.Bytes(), nil
}

func decodeInv(b []byte) ([]invItem, error) {
	r := consensus.NewReader(b)
	n, err := r.VarInt()
	if err != nil {
		return nil, err
	}
	if n > maxInv {
		return nil, errors.New("too many inventory items")
	}
	items := make([]invItem, n)
	for i := range items {
		if items[i].kind, err = r.U32(); err != nil {
			return nil, err
		}
		if items[i].hash, err = r.Hash(); err != nil {
			return nil, err
		}
	}
	if err := r.Empty(); err != nil {
		return nil, err
	}
	return items, nil
}

func encodeGetBlocks(locators []consensus.Hash, stop consensus.Hash) ([]byte, error) {
	if len(locators) > maxLocators {
		return nil, errors.New("too many locators")
	}
	w := consensus.W{}
	w.VarInt(uint64(len(locators)))
	for _, h := range locators {
		w.Hash(h)
	}
	w.Hash(stop)
	return w.Bytes(), nil
}

func decodeGetBlocks(b []byte) ([]consensus.Hash, consensus.Hash, error) {
	r := consensus.NewReader(b)
	n, err := r.VarInt()
	if err != nil {
		return nil, consensus.Hash{}, err
	}
	if n > maxLocators {
		return nil, consensus.Hash{}, errors.New("too many locators")
	}
	loc := make([]consensus.Hash, n)
	for i := range loc {
		if loc[i], err = r.Hash(); err != nil {
			return nil, consensus.Hash{}, err
		}
	}
	stop, err := r.Hash()
	if err != nil {
		return nil, consensus.Hash{}, err
	}
	if err := r.Empty(); err != nil {
		return nil, consensus.Hash{}, err
	}
	return loc, stop, nil
}

func encodeAddr(now uint64, addrs []string) ([]byte, error) {
	if len(addrs) > maxAddr {
		addrs = addrs[:maxAddr]
	}
	w := consensus.W{}
	w.VarInt(uint64(len(addrs)))
	for _, a := range addrs {
		if len(a) > maxAddrLen {
			return nil, errors.New("address too long")
		}
		w.U64(now)
		w.VarBytes([]byte(a))
	}
	return w.Bytes(), nil
}

func decodeAddr(b []byte) ([]string, error) {
	r := consensus.NewReader(b)
	n, err := r.VarInt()
	if err != nil {
		return nil, err
	}
	if n > maxAddr {
		return nil, errors.New("too many addresses")
	}
	out := make([]string, 0, n)
	for i := uint64(0); i < n; i++ {
		if _, err := r.U64(); err != nil {
			return nil, err
		}
		raw, err := r.VarBytes(maxAddrLen)
		if err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			out = append(out, string(raw))
		}
	}
	if err := r.Empty(); err != nil {
		return nil, err
	}
	return out, nil
}

func encodePing(nonce uint64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], nonce)
	return b[:]
}

func decodePing(b []byte) (uint64, error) {
	if len(b) != 8 {
		return 0, errors.New("bad ping")
	}
	return binary.LittleEndian.Uint64(b), nil
}
