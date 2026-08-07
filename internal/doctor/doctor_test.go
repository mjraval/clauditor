package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rishi/clauditor/internal/collect"
	"github.com/rishi/clauditor/internal/config"
)

// TestRunAll_WithStubbinsAndRealGit is the end-to-end path: test/stubbin's
// fake claude/tmux answer the version/agents/daemon checks, and a real temp
// git repo (git itself is never stubbed, per SPEC intent) gives the
// repo/workspace-dir checks something genuine to check against. Access is
// left unconfigured so that check SKIPs cleanly offline (SPEC §16: no test
// touches the network).
func TestRunAll_WithStubbinsAndRealGit(t *testing.T) {
	stubbin, err := filepath.Abs(filepath.Join("..", "..", "test", "stubbin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stubbin, "claude")); err != nil {
		t.Fatalf("stubbin missing: %v", err)
	}
	t.Setenv("PATH", stubbin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Repos = []string{repo}
	cfg.WorkspaceDirs = []string{workspace}
	// Access.TeamDomain left "" — the JWKS check must SKIP, not hit the network.

	runner := collect.NewRunner()
	checks := RunAll(context.Background(), cfg, runner)

	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}

	wantPass := []string{
		"claude --version", // stub: 2.1.223 (Claude Code) -> PASS
		"claude agents --json",
		"claude daemon status", // stub: `claude daemon` -> "pid: 12345", exit 0 -> PASS
		"tmux -V",              // stub: tmux 3.4 -> PASS
		"git --version",        // real git on PATH, expected >= 2.20
		"repo: " + repo,
		"workspace dir: " + workspace,
	}
	for _, name := range wantPass {
		c, ok := byName[name]
		if !ok {
			t.Errorf("missing check %q; got checks: %+v", name, checks)
			continue
		}
		if c.Status != PASS {
			t.Errorf("%s: status = %s, want PASS (detail: %s)", name, c.Status, c.Detail)
		}
	}

	access, ok := byName["access JWKS"]
	if !ok {
		t.Fatal("missing access JWKS check")
	}
	if access.Status != SKIP {
		t.Errorf("access JWKS: status = %s, want SKIP (detail: %s)", access.Status, access.Detail)
	}

	if AnyFail(checks) {
		t.Errorf("expected no FAIL checks in the all-green fixture, got: %+v", checks)
	}
}

// TestAnyFail exercises the aggregate exit-code helper directly.
func TestAnyFail(t *testing.T) {
	allGood := []Check{{Name: "a", Status: PASS}, {Name: "b", Status: WARN}, {Name: "c", Status: SKIP}}
	if AnyFail(allGood) {
		t.Error("AnyFail(no FAIL checks) = true, want false")
	}
	withFail := append(allGood, Check{Name: "d", Status: FAIL})
	if !AnyFail(withFail) {
		t.Error("AnyFail(one FAIL check) = false, want true")
	}
}
