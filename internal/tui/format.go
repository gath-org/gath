package tui

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/gath-org/gath/internal/amount"
)

// FormatHashrate prints hashes per second. measuring is the moment before
// the first sample, while the miner is already running.
func FormatHashrate(hps float64, measuring bool) string {
	if measuring {
		return "measuring"
	}
	if math.IsNaN(hps) || hps < 0 {
		hps = 0
	}
	switch {
	case hps >= 1e9:
		return fmt.Sprintf("%.2f GH/s", hps/1e9)
	case hps >= 1e6:
		return fmt.Sprintf("%.2f MH/s", hps/1e6)
	case hps >= 1e3:
		return fmt.Sprintf("%.2f kH/s", hps/1e3)
	default:
		return fmt.Sprintf("%.0f H/s", hps)
	}
}

func formatAtoms(signed string) string {
	neg := strings.HasPrefix(signed, "-")
	raw := strings.TrimPrefix(signed, "-")
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return signed
	}
	text := amount.Format(n)
	if neg {
		return "-" + text
	}
	if n == 0 {
		return "0"
	}
	return "+" + text
}

func shortID(id string) string {
	if len(id) <= 16 {
		return id
	}
	return id[:16] + "..."
}

func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	prefix := home + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return "~" + string(os.PathSeparator) + path[len(prefix):]
	}
	return path
}

func row(label, value string) string {
	return fmt.Sprintf("%-14s%s", label, value)
}

func wrapWords(s string, width int) string {
	if width < 20 {
		width = 20
	}
	fields := strings.Fields(s)
	var b strings.Builder
	line := 0
	for i, word := range fields {
		if i > 0 && line+1+len(word) > width {
			b.WriteByte('\n')
			line = 0
		} else if i > 0 {
			b.WriteByte(' ')
			line++
		}
		b.WriteString(word)
		line += len(word)
	}
	return b.String()
}

func cleanPhrase(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
