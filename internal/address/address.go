package address

import (
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcutil/bech32"

	"github.com/gath-org/gath/internal/consensus"
)

// Version is the bech32m witness version of a Gath address.
const Version byte = 1

// Encode encodes a public key hash as a bech32m address.
// Mainnet human-readable part is "gath". Devnet is "gathd".
func Encode(hrp string, pkh consensus.Hash) (string, error) {
	if hrp == "" {
		return "", fmt.Errorf("missing address prefix")
	}
	payload, err := bech32.ConvertBits(pkh[:], 8, 5, true)
	if err != nil {
		return "", err
	}
	data := make([]byte, 0, 1+len(payload))
	data = append(data, Version)
	data = append(data, payload...)
	return bech32.EncodeM(hrp, data)
}

// Decode parses a bech32m address. The human-readable part is returned
// lowercased. Mixed case and bech32 (not bech32m) checksums are rejected.
func Decode(addr string) (string, consensus.Hash, error) {
	var zero consensus.Hash
	hrp, data, version, err := bech32.DecodeNoLimitWithVersion(addr)
	if err != nil {
		return "", zero, fmt.Errorf("address: %w", err)
	}
	if version != bech32.VersionM {
		return "", zero, fmt.Errorf("address checksum is not bech32m")
	}
	if len(data) < 1 || data[0] != Version {
		return "", zero, fmt.Errorf("address version is not %d", Version)
	}
	pkhBytes, err := bech32.ConvertBits(data[1:], 5, 8, false)
	if err != nil {
		return "", zero, fmt.Errorf("address: %w", err)
	}
	if len(pkhBytes) != len(zero) {
		return "", zero, fmt.Errorf("address payload must be 32 bytes")
	}
	var pkh consensus.Hash
	copy(pkh[:], pkhBytes)
	return strings.ToLower(hrp), pkh, nil
}
