package amount

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gath-org/gath/internal/consensus"
)

// Parse converts a decimal Gath amount to atoms.
// "1" is one Gath. "0.00000001" is one atom. More than 8 decimal places is rejected.
func Parse(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("missing amount")
	}
	for _, r := range s {
		if r == '.' || unicode.IsDigit(r) {
			continue
		}
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	whole, frac, ok := strings.Cut(s, ".")
	if !ok {
		frac = ""
	} else if strings.Contains(frac, ".") {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	if len(frac) > 8 {
		return 0, fmt.Errorf("amount has more than 8 decimal places")
	}
	if whole == "" {
		whole = "0"
	}
	if frac == "" {
		frac = ""
	}
	w, err := parseDigits(whole)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	for len(frac) < 8 {
		frac += "0"
	}
	f, err := parseDigits(frac)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	if w > (^uint64(0))/consensus.AtomsPerGath {
		return 0, fmt.Errorf("amount overflow")
	}
	atoms := w * consensus.AtomsPerGath
	if atoms > ^uint64(0)-f {
		return 0, fmt.Errorf("amount overflow")
	}
	return atoms + f, nil
}

func parseDigits(s string) (uint64, error) {
	if s == "" {
		return 0, nil
	}
	var n uint64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("digit")
		}
		d := uint64(r - '0')
		if n > (^uint64(0))/10 {
			return 0, fmt.Errorf("overflow")
		}
		n *= 10
		if n > ^uint64(0)-d {
			return 0, fmt.Errorf("overflow")
		}
		n += d
	}
	return n, nil
}

// Format prints atoms as a decimal Gath amount, trimming trailing zeros.
func Format(atoms uint64) string {
	whole := atoms / consensus.AtomsPerGath
	frac := atoms % consensus.AtomsPerGath
	if frac == 0 {
		return fmt.Sprintf("%d", whole)
	}
	s := fmt.Sprintf("%08d", frac)
	s = strings.TrimRight(s, "0")
	return fmt.Sprintf("%d.%s", whole, s)
}
