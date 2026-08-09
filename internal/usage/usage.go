// Package usage estimates per-session token counts and USD cost from a
// Claude Code transcript. The mechanism — reading the per-turn
// `message.usage` object out of
// ${CLAUDE_CONFIG_DIR:-~/.claude}/projects/*/<sessionId>.jsonl and pricing
// it against a table of known models — is adapted from
// references/mux/tmux/claude.go (MIT — Copyright (c) 2026 mux
// contributors; see NOTICE) and re-implemented in Go for clauditor.
//
// Unlike internal/transcript's preview renderer, which only needs the tail
// for a live preview, cost is cumulative over the WHOLE conversation — a
// session that switches models mid-stream (see docs/MESSAGING.md §2) must
// have every turn's usage counted, keyed by the model that served it, so a
// price change or model switch never silently drops tokens from the total.
//
// Parsing follows internal/transcript's tolerant discipline: unknown fields
// are ignored, a corrupt line is skipped rather than fatal, and every field
// is optional. Pricing is separate and deliberately strict: an unpriced
// model reports CostKnown=false rather than guessing a cost of zero — see
// pricing.go.
package usage

import (
	"bytes"
	"encoding/json"
	"io"
	"os"

	"github.com/mjraval/clauditor/internal/transcript"
)

// MaxReadBytes caps how much of a transcript file Compute reads in one
// pass. Cost is cumulative from the first turn, so this wants the WHOLE
// file — but an unbounded read risks stalling a poll cycle on one huge
// session, so anything beyond the cap is dropped from the head (the most
// recent MaxReadBytes are kept) and Truncated is set so callers can say so.
const MaxReadBytes int64 = 32 * 1024 * 1024

// Usage is the aggregated token/cost readout for one session's transcript.
type Usage struct {
	InputTokens       int64
	OutputTokens      int64
	CacheReadTokens   int64
	CacheCreateTokens int64

	// CostMicroUSD is the estimated cost in millionths of a US dollar
	// (int64 microdollar precision; format to USD only at the display
	// edge — see FormatUSD — the agent-deck discipline, docs/MESSAGING.md
	// §2). Trustworthy only when CostKnown is true: when at least one
	// turn used a model absent from the pricing table, the session's true
	// total cost is unknowable, and this field holds only the sum of the
	// turns that WERE priced — callers must gate display on CostKnown,
	// never show this as if it were the whole session's cost.
	CostMicroUSD int64
	CostKnown    bool

	// Truncated is true when the transcript file exceeded MaxReadBytes,
	// so the totals above are a lower bound (most-recent window only),
	// not the full session.
	Truncated bool
}

// Compute resolves sessionID's transcript file (internal/transcript.Resolve)
// and aggregates its usage. ok is false when no transcript exists yet (a
// brand-new session, or one whose project dir hasn't been created).
func Compute(sessionID string) (Usage, bool) {
	path, ok := transcript.Resolve(sessionID)
	if !ok {
		return Usage{}, false
	}
	return ComputeFile(path)
}

// ComputeFile aggregates usage from a specific transcript file — exposed
// for tests and callers (Cache) that already resolved the path.
func ComputeFile(path string) (Usage, bool) {
	data, truncated, err := readCapped(path, MaxReadBytes)
	if err != nil {
		return Usage{}, false
	}
	return parse(data, truncated), true
}

// modelTotals accumulates raw token counts for one model across the
// transcript, before pricing is applied.
type modelTotals struct {
	input, output, cacheRead, cacheCreate int64
}

// wireMessage/wireInner/wireUsage mirror internal/transcript's tolerant
// wire shapes: only the fields Compute needs are decoded, everything else
// (ai-title, mode, attachment, tool blocks, …) is ignored via json.RawMessage.
type wireMessage struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

type wireInner struct {
	Model string     `json:"model"`
	Usage *wireUsage `json:"usage"`
}

type wireUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// parse sums usage per model across every assistant turn in data (manual
// line splitting, not bufio.Scanner, for the same reason as
// internal/transcript.Parse: one oversized line must never truncate the
// rest of the stream), then prices each model's totals and folds the
// result into the returned Usage.
func parse(data []byte, truncated bool) Usage {
	perModel := map[string]modelTotals{}
	for len(data) > 0 {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			line, data = data, nil
		}
		model, u, ok := parseUsageLine(line)
		if !ok {
			continue
		}
		t := perModel[model]
		t.input += u.InputTokens
		t.output += u.OutputTokens
		t.cacheRead += u.CacheReadInputTokens
		t.cacheCreate += u.CacheCreationInputTokens
		perModel[model] = t
	}

	out := Usage{Truncated: truncated, CostKnown: true}
	var costMicro int64
	for model, t := range perModel {
		out.InputTokens += t.input
		out.OutputTokens += t.output
		out.CacheReadTokens += t.cacheRead
		out.CacheCreateTokens += t.cacheCreate

		r, known := Lookup(model)
		if !known {
			// An unpriced model was used: the session's true total cost
			// is unknowable. Keep summing tokens (still accurate) but
			// never claim a complete cost — see the Usage.CostMicroUSD
			// doc comment.
			out.CostKnown = false
			continue
		}
		costMicro += microCost(t.input, r.InputPerMTok)
		costMicro += microCost(t.output, r.OutputPerMTok)
		costMicro += microCost(t.cacheRead, r.CacheReadPerMTok)
		costMicro += microCost(t.cacheCreate, r.CacheWritePerMTok)
	}
	out.CostMicroUSD = costMicro
	return out
}

// parseUsageLine renders one JSONL element into (model, usage, ok). Only
// assistant turns carrying a usage object count; a user turn, an
// ai-title/mode/attachment envelope, a corrupt line, or an assistant turn
// with no usage object (e.g. a pure tool-result echo) all yield ok=false
// and are skipped, never fatal.
func parseUsageLine(raw []byte) (string, wireUsage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", wireUsage{}, false
	}
	var msg wireMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return "", wireUsage{}, false
	}
	if msg.Type != "assistant" {
		return "", wireUsage{}, false
	}
	var inner wireInner
	if err := json.Unmarshal(msg.Message, &inner); err != nil {
		return "", wireUsage{}, false
	}
	if inner.Usage == nil {
		return "", wireUsage{}, false
	}
	u := *inner.Usage
	if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadInputTokens == 0 && u.CacheCreationInputTokens == 0 {
		// All-zero usage carries no cost information. Observed live: a
		// synthetic rate-limit notice (model "<synthetic>") ships a usage
		// object that is present but entirely zero — counting it would
		// mark the whole session CostKnown=false for a model that never
		// actually served a priced turn. Skip rather than let a no-op line
		// poison an otherwise fully-priced session.
		return "", wireUsage{}, false
	}
	return inner.Model, u, true
}

// microCost converts a token count and a $/Mtok rate into microdollars
// (1e-6 USD, rounded half up). A rate of R USD per 1,000,000 tokens costs R
// microdollars per token (1 USD = 1e6 microdollars, so R/1e6 USD/token =
// R microdollars/token) — so N tokens cost N*R microdollars, no division.
func microCost(tokens int64, ratePerMTok float64) int64 {
	if tokens <= 0 || ratePerMTok <= 0 {
		return 0
	}
	return int64(float64(tokens)*ratePerMTok + 0.5)
}

// readCapped reads path whole when it fits within maxBytes, else only the
// last maxBytes (the most recent turns), dropping a leading partial line so
// parse never sees a truncated head line — same discipline as
// internal/transcript's readTail. truncated reports whether any bytes were
// dropped.
func readCapped(path string, maxBytes int64) (data []byte, truncated bool, err error) {
	f, err := os.Open(path) //nolint:gosec // path resolved via transcript.Resolve from a validated session id
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	size := fi.Size()
	start := int64(0)
	if maxBytes > 0 && size > maxBytes {
		start = size - maxBytes
		truncated = true
	}
	if start > 0 {
		if _, err := f.Seek(start, 0); err != nil {
			return nil, false, err
		}
	}
	buf := make([]byte, size-start)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, false, err
	}
	if start > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 && i+1 < len(buf) {
			buf = buf[i+1:]
		}
	}
	return buf, truncated, nil
}
