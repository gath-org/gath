package keys

import (
	"encoding/binary"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"
	"golang.org/x/crypto/sha3"

	"github.com/gath-org/gath/internal/consensus"
)

// DerivationLabel separates Gath ML-DSA seeds from every other use of a
// BIP39 seed. The seed is expanded with SHAKE256, the FIPS 202 XOF.
const DerivationLabel = "gath-mldsa44-v1"

// Key is an ML-DSA-44 key derived from a BIP39 seed and an address index.
type Key struct {
	index uint32
	sk    *mldsa44.PrivateKey
	pk    *mldsa44.PublicKey
	pub   []byte
	pkh   consensus.Hash
}

// Derive expands bip39Seed (the 64-byte BIP39 seed, with an empty BIP39
// passphrase) into the ML-DSA-44 key for index. Index 0 is the wallet's
// address. The 24 words are enough to recover it; there is no extra secret.
func Derive(bip39Seed []byte, index uint32) (*Key, error) {
	if len(bip39Seed) != 64 {
		return nil, errSeed
	}
	h := sha3.NewShake256()
	_, _ = h.Write([]byte(DerivationLabel))
	_, _ = h.Write(bip39Seed)
	var idx [4]byte
	binary.LittleEndian.PutUint32(idx[:], index)
	_, _ = h.Write(idx[:])
	var seed [mldsa44.SeedSize]byte
	_, _ = h.Read(seed[:])
	pk, sk := mldsa44.NewKeyFromSeed(&seed)
	for i := range seed {
		seed[i] = 0
	}
	pub := pk.Bytes()
	return &Key{
		index: index,
		sk:    sk,
		pk:    pk,
		pub:   pub,
		pkh:   consensus.PubKeyHash(pub),
	}, nil
}

// PKH is the public key hash paid to by a transaction output.
func (k *Key) PKH() consensus.Hash { return k.pkh }

// PublicKey is the ML-DSA-44 public key.
func (k *Key) PublicKey() []byte {
	out := make([]byte, len(k.pub))
	copy(out, k.pub)
	return out
}

// Index is the derivation index.
func (k *Key) Index() uint32 { return k.index }

// Sign signs a sighash with the protocol context string.
// The signature is deterministic (FIPS 204 with the random field zero).
func (k *Key) Sign(msg []byte) ([]byte, error) {
	sig := make([]byte, mldsa44.SignatureSize)
	if err := mldsa44.SignTo(k.sk, msg, []byte(consensus.SigContext), false, sig); err != nil {
		return nil, err
	}
	return sig, nil
}

type seedError struct{}

func (seedError) Error() string { return "bip39 seed must be 64 bytes" }

var errSeed = seedError{}
