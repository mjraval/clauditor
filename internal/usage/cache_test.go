package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupSession(t *testing.T, id, content string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	proj := filepath.Join(dir, "projects", "-some-repo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	return dir
}

const oneTurn = `{"type":"assistant","message":{"model":"claude-opus-5","usage":{"input_tokens":1000,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
const twoTurns = oneTurn + `{"type":"assistant","message":{"model":"claude-opus-5","usage":{"input_tokens":1000,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"

func TestCache_MissingSession(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	c := NewCache()
	if _, ok := c.Get("no-such-session"); ok {
		t.Error("Get of an unresolvable session should return ok=false")
	}
}

func TestCache_ServesStaleValueUntilFileChanges(t *testing.T) {
	dir := setupSession(t, "sess-1", oneTurn)
	path := filepath.Join(dir, "projects", "-some-repo", "sess-1.jsonl")

	c := NewCache()
	u1, ok := c.Get("sess-1")
	if !ok || u1.InputTokens != 1000 {
		t.Fatalf("first Get = %+v, %v; want 1000 input tokens", u1, ok)
	}

	// Repeated Get with no file change must keep serving the same value —
	// the whole point of the cache is not re-reading disk on every poll
	// tick when nothing changed.
	u2, ok := c.Get("sess-1")
	if !ok || u2.InputTokens != 1000 {
		t.Fatalf("repeat Get with no file change = %+v, %v; want the same 1000", u2, ok)
	}

	// Now the transcript genuinely grows (a new turn appended, as a real
	// session would) — size AND mtime both change, and the cache must
	// notice and recompute.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	newTime := fi.ModTime().Add(2 * time.Second)
	if err := os.WriteFile(path, []byte(twoTurns), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	u3, ok := c.Get("sess-1")
	if !ok {
		t.Fatal("third Get should still resolve")
	}
	if u3.InputTokens != 2000 {
		t.Errorf("Get should recompute once the transcript file changes: got %d tokens, want 2000", u3.InputTokens)
	}
}
