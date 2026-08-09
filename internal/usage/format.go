package usage

import "fmt"

// FormatUSD renders microdollars as a "$X.XX" string. All cost math stays
// integer microdollars internally; conversion to a dollar string happens
// only here, at the display edge (the agent-deck discipline —
// docs/MESSAGING.md §2).
func FormatUSD(micro int64) string {
	return fmt.Sprintf("$%.2f", float64(micro)/1_000_000)
}

// FormatTokens renders a token count as a short human string: "1.2M",
// "450k", "820".
func FormatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
