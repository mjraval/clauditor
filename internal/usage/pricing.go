package usage

// PricingAsOf is the date this table was last checked against Anthropic's
// published list prices (platform.claude.com/docs/en/pricing). Anthropic
// changes prices — including scheduled step-changes announced in advance,
// see the claude-sonnet-5 note below — so this constant and the table
// beneath it MUST be updated whenever pricing changes. A stale table is
// worse than one that's honestly incomplete: surface staleness, don't hide
// it (the agent-deck lesson, docs/MESSAGING.md §2/§4.2).
const PricingAsOf = "2026-08-09"

// Rate is Anthropic's list price for one model, in USD per million tokens.
type Rate struct {
	InputPerMTok      float64
	OutputPerMTok     float64
	CacheReadPerMTok  float64
	CacheWritePerMTok float64 // 5-minute ephemeral cache write (the default TTL)
}

// rate derives cache read/write rates from Anthropic's documented FIXED
// ratio to the base input rate — cache reads run ~0.1x input, 5-minute
// cache writes ~1.25x input, per platform.claude.com/docs/en/build-with-
// claude/prompt-caching "Economics" — rather than a separate per-model
// number, which Anthropic does not publish.
func rate(inputPerMTok, outputPerMTok float64) Rate {
	return Rate{
		InputPerMTok:      inputPerMTok,
		OutputPerMTok:     outputPerMTok,
		CacheReadPerMTok:  inputPerMTok * 0.1,
		CacheWritePerMTok: inputPerMTok * 1.25,
	}
}

// pricing is the current per-model rate table, current as of PricingAsOf.
// Only models with a confirmed published list price are listed. An
// unlisted model — a legacy/deprecated alias, a future model, a stale
// dated snapshot ID — is priced as CostKnown=false by callers (see
// Usage.CostMicroUSD), never guessed at $0.
//
// claude-sonnet-5 is priced at Anthropic's INTRODUCTORY rate ($2/$10 per
// Mtok), active through 2026-08-31; it reverts to the $3/$15 standard rate
// on 2026-09-01. Whoever owns this file after that date must flip the line
// below back to rate(3.00, 15.00).
//
// claude-haiku-4-5 additionally carries a dated-snapshot alias
// (claude-haiku-4-5-20251001): unlike the other current models, real
// transcripts log Haiku's resolved dated ID rather than the bare alias
// (verified live against ~/.claude/projects/*.jsonl), so both must be
// priced identically or every Haiku turn reports CostKnown=false.
var pricing = map[string]Rate{
	"claude-fable-5":            rate(10.00, 50.00),
	"claude-mythos-5":           rate(10.00, 50.00),
	"claude-opus-5":             rate(5.00, 25.00),
	"claude-opus-4-8":           rate(5.00, 25.00),
	"claude-opus-4-7":           rate(5.00, 25.00),
	"claude-opus-4-6":           rate(5.00, 25.00),
	"claude-sonnet-5":           rate(2.00, 10.00), // introductory through 2026-08-31 — see note above
	"claude-sonnet-4-6":         rate(3.00, 15.00),
	"claude-haiku-4-5":          rate(1.00, 5.00),
	"claude-haiku-4-5-20251001": rate(1.00, 5.00),
}

// Lookup returns the rate for model, or ok=false when the model has no
// confirmed price in the table.
func Lookup(model string) (Rate, bool) {
	r, ok := pricing[model]
	return r, ok
}
