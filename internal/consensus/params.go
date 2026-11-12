package consensus

import (
	"fmt"
	"math/big"
	"sync"
)

const (
	// AtomsPerGath is the number of indivisible base units in one Gath.
	AtomsPerGath uint64 = 100_000_000

	// InitialSubsidyAtoms is the block subsidy before the first halving (5 Gath).
	InitialSubsidyAtoms uint64 = 5 * AtomsPerGath

	// HalvingInterval is the number of blocks between subsidy halvings.
	// At a 45s target this is exactly 3 * 365 days.
	HalvingInterval uint64 = 2_100_000

	// TargetSpacing is the aimed time between blocks, in seconds.
	TargetSpacing uint64 = 45

	// ASERTHalfLife is the relative-ASERT half-life in seconds (288 blocks).
	ASERTHalfLife uint64 = 12_960

	// CoinbaseMaturity is the number of blocks before a coinbase output can be spent.
	// A coinbase created at height h may be spent in a block at height >= h+CoinbaseMaturity.
	CoinbaseMaturity uint64 = 100

	// MaxBlockBytes is the maximum serialized block size, header included.
	// ML-DSA-44 signatures are 2420 bytes, so a payment is a few kilobytes.
	// 4 MiB is about a thousand simple payments per block.
	MaxBlockBytes = 4_000_000

	// MaxTxBytes is the maximum serialized transaction size.
	MaxTxBytes = 100_000

	// MinFeePerByte is the consensus minimum fee, in atoms per serialized byte.
	MinFeePerByte uint64 = 1

	// MinOutputAtoms is the smallest non-coinbase output.
	// It is above the minimum fee to spend one input, so an output is never
	// created cheaper than it costs to clean up. Coinbase outputs may be
	// smaller so the tail of the subsidy schedule remains claimable.
	MinOutputAtoms uint64 = 4_000

	// MaxFutureSeconds is how far ahead of the local clock a block timestamp may be.
	MaxFutureSeconds int64 = 2 * 60 * 60

	// MedianTimeBlocks is the median-time-past window.
	MedianTimeBlocks = 11

	// MaxCoinbaseScript is the maximum coinbase script length.
	MaxCoinbaseScript = 100

	// MaxInputs and MaxOutputs bound transaction fan-out.
	MaxInputs  = 1024
	MaxOutputs = 1024

	// MaxBlockTxs bounds the transaction count independently of byte size.
	MaxBlockTxs = 50_000

	// TxVersion is the only transaction version accepted by this milestone.
	TxVersion uint32 = 1

	// BlockVersion is the only block version accepted by this milestone.
	BlockVersion uint32 = 1

	// LockTimeMust is the only legal lock time in version 1.
	LockTimeMust uint32 = 0

	// SequenceMust is the only legal input sequence in version 1.
	SequenceMust uint32 = 0xffffffff

	// CoinbaseIndex is the previous-output index of a coinbase input.
	CoinbaseIndex uint32 = 0xffffffff

	// SigContext is the FIPS 204 context string bound into every signature.
	SigContext = "gath-v1"

	// MainnetMagic and DevnetMagic identify the P2P network.
	MainnetMagic uint32 = 0x47415448 // "GATH"
	DevnetMagic  uint32 = 0x47415444 // "GATD"

	// GenesisTime is 2026-11-10 00:00:00 UTC, the date of the design draft.
	// Relative ASERT does not schedule issuance from this instant, so a node
	// started later does not grind through a backlog of minimum-difficulty blocks.
	GenesisTime uint64 = 1_794_268_800

	// MainnetPowBits is the easiest legal mainnet target.
	// Compact 0x1d200000 encodes 2^229. On the reference Go hasher
	// (about 3.5e6 SHA3-256 header hashes per second per core) that is
	// roughly 40 seconds of work.
	MainnetPowBits uint32 = 0x1d200000

	// DevnetPowBits is the easiest legal devnet target (just under 2^255).
	// A block solves in one or two hashes so local networks and tests run
	// without waiting on the clock. The adjustment algorithm is the same
	// one used on mainnet.
	DevnetPowBits uint32 = 0x207fffff
)

// Network identifies a parameter set baked into the binary.
type Network uint8

const (
	Mainnet Network = iota
	Devnet
)

func (n Network) String() string {
	switch n {
	case Mainnet:
		return "mainnet"
	case Devnet:
		return "devnet"
	default:
		panic(fmt.Sprintf("unknown network %d", n))
	}
}

// ParseNetwork reads "mainnet" or "devnet".
func ParseNetwork(s string) (Network, error) {
	switch s {
	case "mainnet":
		return Mainnet, nil
	case "devnet":
		return Devnet, nil
	default:
		return 0, fmt.Errorf("unknown network %q (use mainnet or devnet)", s)
	}
}

// Params are the consensus parameters of one network.
// They are fixed in the binary. There is no admin path that changes them.
type Params struct {
	Network Network
	Name    string
	Magic   uint32
	HRP     string

	TargetSpacing uint64
	ASERTHalfLife uint64
	PowLimitBits  uint32
	GenesisTime   uint64
	GenesisNonce  uint64
	GenesisTag    string
	P2PPort       int
	RPCPort       int

	genesis  *Block
	powLimit *big.Int
}

// PowLimit returns the easiest legal target.
func (p *Params) PowLimit() *big.Int {
	return new(big.Int).Set(p.powLimit)
}

var (
	mainOnce sync.Once
	mainP    *Params
	devOnce  sync.Once
	devP     *Params
)

// ByNetwork returns the parameter set for a network.
func ByNetwork(n Network) *Params {
	switch n {
	case Mainnet:
		return mainnetParams()
	case Devnet:
		return devnetParams()
	default:
		panic(fmt.Sprintf("unknown network %d", n))
	}
}

func mainnetParams() *Params {
	mainOnce.Do(func() {
		mainP = newMainnet()
	})
	return mainP
}

func devnetParams() *Params {
	devOnce.Do(func() {
		devP = newDevnet()
	})
	return devP
}

func newMainnet() *Params {
	p := &Params{
		Network:       Mainnet,
		Name:          "mainnet",
		Magic:         MainnetMagic,
		HRP:           "gath",
		TargetSpacing: TargetSpacing,
		ASERTHalfLife: ASERTHalfLife,
		PowLimitBits:  MainnetPowBits,
		GenesisTime:   GenesisTime,
		// Mined for this exact genesis header. See TestGenesisPow.
		GenesisNonce: 149_223_004,
		GenesisTag:   "Gath genesis. No issuer. No premine. November 2026.",
		P2PPort:      9733,
		RPCPort:      9734,
	}
	p.mustInit()
	return p
}

func newDevnet() *Params {
	p := &Params{
		Network:       Devnet,
		Name:          "devnet",
		Magic:         DevnetMagic,
		HRP:           "gathd",
		TargetSpacing: TargetSpacing,
		ASERTHalfLife: ASERTHalfLife,
		PowLimitBits:  DevnetPowBits,
		GenesisTime:   GenesisTime,
		// Mined for this exact genesis header. See TestGenesisPow.
		GenesisNonce: 11,
		GenesisTag:   "Gath devnet genesis. No issuer. No premine.",
		P2PPort:      19733,
		RPCPort:      19734,
	}
	p.mustInit()
	return p
}

func (p *Params) mustInit() {
	limit, err := CompactToTarget(p.PowLimitBits)
	if err != nil {
		panic(err)
	}
	p.powLimit = limit
	p.genesis = buildGenesis(p)
}

func buildGenesis(p *Params) *Block {
	script := make([]byte, 4+len(p.GenesisTag))
	// height 0
	copy(script[4:], p.GenesisTag)
	tx := &Tx{
		Version: TxVersion,
		Inputs: []TxIn{{
			Prev:     Outpoint{Index: CoinbaseIndex},
			Sequence: SequenceMust,
			Script:   script,
		}},
		LockTime: LockTimeMust,
	}
	block := &Block{
		Header: BlockHeader{
			Version: BlockVersion,
			Time:    p.GenesisTime,
			Bits:    p.PowLimitBits,
			Nonce:   p.GenesisNonce,
		},
		Txs: []*Tx{tx},
	}
	block.Header.Merkle = MerkleRoot([]Hash{tx.ID()})
	return block
}

// Genesis returns the network genesis block. It has no spendable outputs.
func (p *Params) Genesis() *Block {
	return p.genesis
}

// GenesisHash is the header hash of the genesis block.
func (p *Params) GenesisHash() Hash {
	return p.genesis.Hash()
}
