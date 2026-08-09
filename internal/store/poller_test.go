package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mjraval/clauditor/internal/config"
	"github.com/mjraval/clauditor/internal/model"
)

const fixtureTurn = `{"type":"assistant","message":{"model":"claude-opus-5","usage":{"input_tokens":1000,"output_tokens":200,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"

// setupTranscript writes a minimal transcript for sessionID under a fresh
// CLAUDE_CONFIG_DIR so internal/usage (via internal/transcript.Resolve) can
// find it, mirroring the pattern in internal/usage's own tests.
func setupTranscript(t *testing.T, sessionID string) {
	t.Helper()
	dir := t.TempDir()
	proj := filepath.Join(dir, "projects", "-some-repo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, sessionID+".jsonl"), []byte(fixtureTurn), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
}

func TestEnrichUsage_OffByDefault(t *testing.T) {
	setupTranscript(t, "sess-1")
	p := &Poller{Cfg: &config.Config{}} // Usage.TrackCost defaults false
	snap := &model.Snapshot{Sessions: []*model.Session{{Key: "k", SessionID: "sess-1"}}}

	p.enrichUsage(snap)

	s := snap.Sessions[0]
	if s.Tokens != 0 || s.CostMicroUSD != 0 || s.CostKnown {
		t.Errorf("enrichUsage should be a no-op when track_cost is off: %+v", s)
	}
}

func TestEnrichUsage_PopulatesWhenEnabled(t *testing.T) {
	setupTranscript(t, "sess-1")
	p := &Poller{Cfg: &config.Config{Usage: config.Usage{TrackCost: true}}}
	snap := &model.Snapshot{Sessions: []*model.Session{{Key: "k", SessionID: "sess-1"}}}

	p.enrichUsage(snap)

	s := snap.Sessions[0]
	if s.Tokens != 1200 { // 1000 input + 200 output
		t.Errorf("Tokens = %d, want 1200", s.Tokens)
	}
	if !s.CostKnown {
		t.Error("CostKnown should be true for a session using a priced model")
	}
	if s.CostMicroUSD <= 0 {
		t.Errorf("CostMicroUSD should be positive, got %d", s.CostMicroUSD)
	}
}

func TestEnrichUsage_SkipsSessionsWithoutSessionID(t *testing.T) {
	p := &Poller{Cfg: &config.Config{Usage: config.Usage{TrackCost: true}}}
	snap := &model.Snapshot{Sessions: []*model.Session{{Key: "k"}}} // no SessionID

	p.enrichUsage(snap) // must not panic on transcript.Resolve("")

	s := snap.Sessions[0]
	if s.Tokens != 0 || s.CostKnown {
		t.Errorf("session with no SessionID should be left untouched: %+v", s)
	}
}

func TestEnrichUsage_NilSnapshotSafe(t *testing.T) {
	p := &Poller{Cfg: &config.Config{Usage: config.Usage{TrackCost: true}}}
	p.enrichUsage(nil) // must not panic
}

// TestEnrichUsage_CacheReusedAcrossCalls exercises the (sessionID,
// file-mtime+size) caching behind enrichUsage — a second call with an
// unchanged transcript should be cheap and yield the same numbers (the
// point of the cache: no re-read every poll tick when nothing changed).
func TestEnrichUsage_CacheReusedAcrossCalls(t *testing.T) {
	setupTranscript(t, "sess-1")
	p := &Poller{Cfg: &config.Config{Usage: config.Usage{TrackCost: true}}}
	snap := &model.Snapshot{Sessions: []*model.Session{{Key: "k", SessionID: "sess-1"}}}

	p.enrichUsage(snap)
	first := snap.Sessions[0].Tokens

	p.enrichUsage(snap)
	second := snap.Sessions[0].Tokens

	if first != second || first != 1200 {
		t.Errorf("repeat enrichUsage calls should agree: first=%d second=%d, want 1200", first, second)
	}
	if p.usageCache == nil {
		t.Error("enrichUsage should lazily create the usage cache")
	}
}
