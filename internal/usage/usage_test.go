package usage

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mjraval/clauditor/internal/transcript"
)

// TestComputeFile_KnownModels covers a multi-turn transcript that switches
// models mid-conversation (opus-5 → haiku-4-5), asserting exact token sums
// and a known cost calculation.
func TestComputeFile_KnownModels(t *testing.T) {
	u, ok := ComputeFile(filepath.Join("testdata", "known.jsonl"))
	if !ok {
		t.Fatal("ComputeFile returned ok=false for a real file")
	}
	if u.InputTokens != 3500 || u.OutputTokens != 1400 || u.CacheReadTokens != 500 || u.CacheCreateTokens != 250 {
		t.Errorf("token sums = %+v, want input=3500 output=1400 cacheRead=500 cacheCreate=250", u)
	}
	if !u.CostKnown {
		t.Fatal("CostKnown should be true when every turn's model is priced")
	}
	// opus-5 turns: 18225 + 31088 = 49313; haiku-4-5 turn: 1000. See
	// usage_test.go's doc math (also re-derived in TestMicroCost_RoundsHalfUp).
	const want = 49313 + 1000
	if u.CostMicroUSD != want {
		t.Errorf("CostMicroUSD = %d, want %d", u.CostMicroUSD, want)
	}
	if u.Truncated {
		t.Error("small fixture should not report Truncated")
	}
}

// TestComputeFile_ModelSwitchMissingUsageCorruptLineUnknownModel exercises
// every tolerant-parsing case in one fixture: a model switch mid-session, a
// missing usage object, a corrupt line, and an unpriced model. Token sums
// must still be accurate (tokens are never dropped for pricing reasons);
// CostKnown must go false because of the unpriced model, but the
// known-priced partial sum is preserved rather than zeroed.
func TestComputeFile_ModelSwitchMissingUsageCorruptLineUnknownModel(t *testing.T) {
	u, ok := ComputeFile(filepath.Join("testdata", "mixed.jsonl"))
	if !ok {
		t.Fatal("ComputeFile returned ok=false for a real file")
	}
	if u.InputTokens != 3510 || u.OutputTokens != 1405 || u.CacheReadTokens != 500 || u.CacheCreateTokens != 250 {
		t.Errorf("token sums = %+v, want input=3510 output=1405 cacheRead=500 cacheCreate=250", u)
	}
	if u.CostKnown {
		t.Error("CostKnown should be false once an unpriced model appears in the transcript")
	}
	// The unknown-model turn (10 input / 5 output tokens) must not have
	// been guessed as zero-cost and folded silently into a "complete"
	// total — but the known-priced opus-5+haiku-4-5 partial sum survives.
	const want = 49313 + 1000
	if u.CostMicroUSD != want {
		t.Errorf("CostMicroUSD = %d, want the known-priced partial sum %d", u.CostMicroUSD, want)
	}
}

func TestComputeFile_MissingFile(t *testing.T) {
	if _, ok := ComputeFile(filepath.Join("testdata", "does-not-exist.jsonl")); ok {
		t.Error("ComputeFile of a missing path should return ok=false")
	}
}

func TestCompute_UsesTranscriptResolve(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "projects", "-some-repo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "sess-abc"
	data, err := os.ReadFile(filepath.Join("testdata", "known.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	u, ok := Compute(id)
	if !ok {
		t.Fatal("Compute should resolve the session via internal/transcript.Resolve")
	}
	if u.InputTokens != 3500 {
		t.Errorf("InputTokens = %d, want 3500", u.InputTokens)
	}

	if _, ok := Compute("no-such-session"); ok {
		t.Error("Compute of an unresolvable session id should return ok=false")
	}
	// Exercise Resolve directly to be sure the fixture really lives where
	// Compute expects (keeps this test honest about what it's covering).
	if _, ok := transcript.Resolve(id); !ok {
		t.Fatal("fixture setup: transcript.Resolve should find the written file")
	}
}

func TestParseUsageLine(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantModel string
		wantOK    bool
	}{
		{"assistant with usage", `{"type":"assistant","message":{"model":"claude-opus-5","usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}}`,
			"claude-opus-5", true},
		{"user turn skipped", `{"type":"user","message":{"role":"user","content":"hi"}}`, "", false},
		{"assistant missing usage skipped", `{"type":"assistant","message":{"model":"claude-opus-5"}}`, "", false},
		{"corrupt json skipped", `{not json`, "", false},
		{"empty line skipped", ``, "", false},
		{"unknown top type skipped", `{"type":"ai-title","title":"t"}`, "", false},
		// Observed live: a synthetic rate-limit notice ships model
		// "<synthetic>" with a usage object present but entirely zero.
		// Counting it would flip CostKnown=false for a fully-priced
		// session over a line that carries no cost information at all.
		{"all-zero usage skipped (synthetic rate-limit notice)",
			`{"type":"assistant","message":{"model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`,
			"", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			model, u, ok := parseUsageLine([]byte(c.raw))
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && model != c.wantModel {
				t.Errorf("model = %q, want %q", model, c.wantModel)
			}
			if ok && c.name == "assistant with usage" {
				if u.InputTokens != 1 || u.OutputTokens != 2 || u.CacheReadInputTokens != 3 || u.CacheCreationInputTokens != 4 {
					t.Errorf("usage = %+v, want {1 2 3 4}", u)
				}
			}
		})
	}
}

func TestMicroCost_RoundsHalfUp(t *testing.T) {
	// 150 tokens * 6.25 $/Mtok = 937.5 microdollars → rounds to 938.
	if got := microCost(150, 6.25); got != 938 {
		t.Errorf("microCost(150, 6.25) = %d, want 938", got)
	}
	if got := microCost(0, 5.0); got != 0 {
		t.Errorf("microCost(0, 5.0) = %d, want 0", got)
	}
	if got := microCost(1000, 5.0); got != 5000 {
		t.Errorf("microCost(1000, 5.0) = %d, want 5000", got)
	}
}

// TestParse_OversizedLineDoesNotTruncateStream mirrors the internal/
// transcript regression: one corrupt/oversized line must not drop later
// lines' usage.
func TestParse_OversizedLineDoesNotTruncateStream(t *testing.T) {
	huge := `{"type":"assistant","message":{"model":"claude-opus-5","usage":` + strings.Repeat(" ", 9*1024*1024) + `not-json`
	after := `{"type":"assistant","message":{"model":"claude-haiku-4-5","usage":{"input_tokens":7,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`
	data := []byte(huge + "\n" + after + "\n")
	u := parse(data, false)
	if u.InputTokens != 7 {
		t.Errorf("InputTokens = %d, want 7 (the huge corrupt line must not drop the following one)", u.InputTokens)
	}
}

func TestReadCapped_TruncatesTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.jsonl")
	var buf bytes.Buffer
	line := strings.Repeat("x", 100) + "\n"
	for i := 0; i < 50; i++ {
		buf.WriteString(line)
	}
	marker := "MARKER-LAST-LINE\n"
	buf.WriteString(marker)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	// Cap tight enough to force truncation but big enough to keep the tail.
	data, truncated, err := readCapped(path, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Error("expected truncated=true when the file exceeds maxBytes")
	}
	if !strings.Contains(string(data), "MARKER-LAST-LINE") {
		t.Errorf("truncated read should keep the tail (most recent content): %q", data)
	}

	// Full read when the file fits.
	data2, truncated2, err := readCapped(path, int64(buf.Len())+1000)
	if err != nil {
		t.Fatal(err)
	}
	if truncated2 {
		t.Error("expected truncated=false when the file fits under maxBytes")
	}
	if len(data2) != buf.Len() {
		t.Errorf("full read length = %d, want %d", len(data2), buf.Len())
	}
}
