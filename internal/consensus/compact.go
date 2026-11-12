package consensus

import (
	"math/big"
)

// CompactToTarget decodes a Bitcoin-style nBits word.
// Negative and overflow encodings are rejected. A zero target is rejected.
func CompactToTarget(bits uint32) (*big.Int, error) {
	mantissa := bits & 0x007fffff
	negative := bits&0x00800000 != 0
	size := bits >> 24
	if negative {
		return nil, rule("bad-bits", "negative target")
	}
	if mantissa == 0 {
		return nil, rule("bad-bits", "zero target")
	}
	// Overflow matches Bitcoin's arith_uint256::SetCompact.
	if size > 34 || (mantissa > 0xff && size > 33) || (mantissa > 0xffff && size > 32) {
		return nil, rule("bad-bits", "target overflow")
	}
	target := new(big.Int).SetUint64(uint64(mantissa))
	if size <= 3 {
		shift := 8 * (3 - size)
		target.Rsh(target, uint(shift))
	} else {
		shift := 8 * (size - 3)
		target.Lsh(target, uint(shift))
	}
	if target.Sign() <= 0 {
		return nil, rule("bad-bits", "zero target")
	}
	return target, nil
}

// TargetToCompact encodes target as a canonical Bitcoin-style nBits word.
func TargetToCompact(target *big.Int) uint32 {
	if target.Sign() <= 0 {
		return 0
	}
	b := target.Bytes() // big-endian, no leading zeros
	size := uint32(len(b))
	var compact uint32
	if size <= 3 {
		compact = uint32(new(big.Int).Lsh(target, uint(8*(3-size))).Uint64())
	} else {
		shifted := new(big.Int).Rsh(target, uint(8*(size-3)))
		compact = uint32(shifted.Uint64())
	}
	if compact&0x00800000 != 0 {
		compact >>= 8
		size++
	}
	compact |= size << 24
	return compact
}

// HashMeetsTarget reports whether hash, read as a big-endian integer, is
// less than or equal to target.
func HashMeetsTarget(hash Hash, target *big.Int) bool {
	if target == nil || target.Sign() <= 0 {
		return false
	}
	h := new(big.Int).SetBytes(hash[:])
	return h.Cmp(target) <= 0
}

// BlockWork returns the chainwork contribution of a block with this target:
// floor( (2^256 - target - 1) / (target + 1) ), matching Bitcoin.
func BlockWork(target *big.Int) *big.Int {
	num := new(big.Int).Lsh(big.NewInt(1), 256)
	num.Sub(num, big.NewInt(1))
	num.Sub(num, target)
	den := new(big.Int).Add(new(big.Int).Set(target), big.NewInt(1))
	return num.Quo(num, den)
}

// Difficulty is powLimit/target, the factor above minimum difficulty.
// The result is a decimal string with up to 8 fractional digits.
func Difficulty(target, powLimit *big.Int) string {
	if target.Sign() <= 0 {
		return "0"
	}
	// scale by 1e8 so the string keeps a fractional part without float.
	num := new(big.Int).Mul(powLimit, big.NewInt(100_000_000))
	q, r := new(big.Int).QuoRem(num, target, new(big.Int))
	_ = r
	whole := new(big.Int).Quo(q, big.NewInt(100_000_000))
	frac := new(big.Int).Mod(q, big.NewInt(100_000_000))
	return whole.String() + "." + leftPad(frac.String(), 8)
}

func leftPad(s string, n int) string {
	for len(s) < n {
		s = "0" + s
	}
	return s
}
