package consensus

import "math/big"

// asertMaxShift bounds the fixed-point shift. Beyond this the result is
// outside (1, powLimit] for every legal target, so clamping is exact.
const asertMaxShift = 512

// CalcASERT evaluates the aserti3-2d fixed-point step.
//
//	next = refTarget * 2^((timeDiff - spacing*(heightDiff+1)) / halfLife)
//
// using the integer cubic approximation from the ASERT paper / Bitcoin Cash
// aserti3-2d (error under 0.013% on the fractional part). Division truncates
// toward zero. The result is clamped to [1, powLimit].
//
// Gath calls this in relative form: refTarget is the parent target, heightDiff
// is 0, and timeDiff is the parent block's interval (parent.time -
// grandparent.time). See NextTarget.
func CalcASERT(refTarget *big.Int, spacing, timeDiff, heightDiff, halfLife int64, powLimit *big.Int) (*big.Int, error) {
	if refTarget == nil || refTarget.Sign() <= 0 {
		return nil, rule("bad-bits", "reference target")
	}
	if heightDiff < 0 {
		return nil, rule("bad-bits", "negative height delta")
	}
	if halfLife <= 0 || spacing <= 0 {
		return nil, rule("bad-bits", "bad asert parameters")
	}
	// ideal blocks from the anchor through the block being targeted.
	ideal := spacing * (heightDiff + 1)
	diff := timeDiff - ideal
	// Keep the exponent inside int64. Real intervals are seconds, not 2^46.
	const maxDiff = int64(1) << 46
	if diff > maxDiff || diff < -maxDiff {
		return nil, rule("bad-time", "asert time delta out of range")
	}
	exponent := (diff * 65536) / halfLife

	shifts := exponent >> 16
	frac := int64(uint16(exponent))

	// factor = 65536 + ((c1*f + c2*f^2 + c3*f^3 + 2^47) >> 48)
	f := big.NewInt(frac)
	f2 := new(big.Int).Mul(f, f)
	f3 := new(big.Int).Mul(f2, f)
	sum := new(big.Int).Mul(big.NewInt(195766423245049), f)
	sum.Add(sum, new(big.Int).Mul(big.NewInt(971821376), f2))
	sum.Add(sum, new(big.Int).Mul(big.NewInt(5127), f3))
	sum.Add(sum, new(big.Int).Lsh(big.NewInt(1), 47))
	sum.Rsh(sum, 48)
	factor := sum.Add(sum, big.NewInt(65536))

	next := new(big.Int).Mul(refTarget, factor)
	shifts -= 16
	switch {
	case shifts <= -asertMaxShift:
		next = big.NewInt(1)
	case shifts < 0:
		next.Rsh(next, uint(-shifts))
	case shifts > asertMaxShift:
		next = new(big.Int).Set(powLimit)
	case shifts > 0:
		next.Lsh(next, uint(shifts))
	}
	if next.Sign() == 0 {
		next = big.NewInt(1)
	}
	if next.Cmp(powLimit) > 0 {
		next = new(big.Int).Set(powLimit)
	}
	return next, nil
}

// NextTarget is the proof-of-work target for the block that extends parent.
// The genesis block and the block directly above it use powLimit. Later blocks
// use relative ASERT against the parent target and the parent interval.
func NextTarget(parent *BlockHeader, parentHeight uint64, grandparent *BlockHeader, p *Params) (*big.Int, error) {
	limit, err := CompactToTarget(p.PowLimitBits)
	if err != nil {
		return nil, err
	}
	if parent == nil || parentHeight == 0 {
		return limit, nil
	}
	parentTarget, err := CompactToTarget(parent.Bits)
	if err != nil {
		return nil, err
	}
	if grandparent == nil {
		return nil, rule("bad-bits", "missing grandparent")
	}
	interval := int64(parent.Time) - int64(grandparent.Time)
	return CalcASERT(parentTarget, int64(p.TargetSpacing), interval, 0, int64(p.ASERTHalfLife), limit)
}

// NextBits is the canonical compact target for the block extending parent.
func NextBits(parent *BlockHeader, parentHeight uint64, grandparent *BlockHeader, p *Params) (uint32, error) {
	target, err := NextTarget(parent, parentHeight, grandparent, p)
	if err != nil {
		return 0, err
	}
	return TargetToCompact(target), nil
}
