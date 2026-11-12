package consensus

// Subsidy is the coinbase subsidy of a block at height, in atoms.
// Height 0 (genesis) is zero. The schedule then emits InitialSubsidyAtoms
// and halves every HalvingInterval blocks, using integer division, until it
// reaches zero. The sum of every subsidy is MaxSupplyAtoms.
func Subsidy(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	era := (height - 1) / HalvingInterval
	if era >= 63 {
		return 0
	}
	return InitialSubsidyAtoms >> era
}

// MaxSupplyAtoms is the exact sum of Subsidy over all heights.
// The geometric series of 5 Gath, halved by integer shift every 2,100,000
// blocks, is 20,999,999.727 Gath. The 0.273 Gath gap from 21,000,000 is the
// atoms lost to integer halving. Consensus enforces the schedule, so this
// sum is the hard cap.
const MaxSupplyAtoms uint64 = 2_099_999_972_700_000
