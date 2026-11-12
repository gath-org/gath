package consensus

import (
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/sha3"
)

// Hash is a SHA3-256 digest. Byte order is the natural big-endian order of
// the digest: the integer value used for proof-of-work is Hash interpreted as
// a big-endian integer, and String prints that same order.
type Hash [32]byte

func (h Hash) String() string { return hex.EncodeToString(h[:]) }

func (h Hash) IsZero() bool {
	for _, b := range h {
		if b != 0 {
			return false
		}
	}
	return true
}

// SHA3 returns SHA3-256 of b.
func SHA3(b []byte) Hash {
	return sha3.Sum256(b)
}

// ParseHash decodes a 64-character hex digest.
func ParseHash(s string) (Hash, error) {
	var h Hash
	if len(s) != 64 {
		return h, fmt.Errorf("hash must be 64 hex characters")
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return h, fmt.Errorf("hash: %w", err)
	}
	copy(h[:], raw)
	return h, nil
}

// ErrNonMinimal is returned when a compact integer is not in canonical form.
var ErrNonMinimal = errors.New("non-minimal varint")
