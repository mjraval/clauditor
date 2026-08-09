package usage

import "testing"

func TestLookup_KnownModel(t *testing.T) {
	r, ok := Lookup("claude-opus-5")
	if !ok {
		t.Fatal("claude-opus-5 should be priced")
	}
	if r.InputPerMTok != 5.00 || r.OutputPerMTok != 25.00 {
		t.Errorf("opus-5 rate = %+v, want input=5.00 output=25.00", r)
	}
}

func TestLookup_UnknownModel(t *testing.T) {
	if _, ok := Lookup("claude-does-not-exist"); ok {
		t.Error("an unpriced model must report ok=false, never a guessed rate")
	}
}

// TestRate_DerivesCacheRatiosFromInput pins the documented cache-pricing
// ratio (read ≈0.1x input, 5-minute write ≈1.25x input) so a future edit to
// the pricing table can't silently drift the cache math.
func TestRate_DerivesCacheRatiosFromInput(t *testing.T) {
	r := rate(4.0, 20.0)
	if r.CacheReadPerMTok != 0.4 {
		t.Errorf("CacheReadPerMTok = %v, want 0.4 (0.1x input)", r.CacheReadPerMTok)
	}
	if r.CacheWritePerMTok != 5.0 {
		t.Errorf("CacheWritePerMTok = %v, want 5.0 (1.25x input)", r.CacheWritePerMTok)
	}
}

// TestPricing_CoversCurrentFamilies is a coverage smoke test, not a full
// enumeration — it just guards against silently losing a row.
func TestPricing_CoversCurrentFamilies(t *testing.T) {
	for _, m := range []string{
		"claude-opus-5", "claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6",
		"claude-sonnet-5", "claude-sonnet-4-6",
		"claude-haiku-4-5", "claude-haiku-4-5-20251001",
		"claude-fable-5", "claude-mythos-5",
	} {
		if _, ok := Lookup(m); !ok {
			t.Errorf("expected %s to be in the pricing table", m)
		}
	}
}
