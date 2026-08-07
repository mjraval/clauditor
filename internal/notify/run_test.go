package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rishi/clauditor/internal/collect"
	"github.com/rishi/clauditor/internal/config"
)

// TestRunOnce_WithStubbins is the M1 acceptance path: the real collectors
// exec the fake `claude`/`tmux` from test/stubbin (prepended to PATH), and
// two --once invocations with different fixtures produce a state diff.
func TestRunOnce_WithStubbins(t *testing.T) {
	stubbin, err := filepath.Abs(filepath.Join("..", "..", "test", "stubbin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stubbin, "claude")); err != nil {
		t.Fatalf("stubbin missing: %v", err)
	}
	t.Setenv("PATH", stubbin+string(os.PathListSeparator)+os.Getenv("PATH"))

	stateDir := t.TempDir()
	callLog := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("CLAUDITOR_STUB_LOG", callLog)

	fixtures := t.TempDir()
	prev := `[{"pid":1,"id":"aa","cwd":"/tmp/x","kind":"background","startedAt":1786000000000,"sessionId":"aa-full","name":"probe","status":"busy","state":"working"}]`
	cur := `[{"pid":1,"id":"aa","cwd":"/tmp/x","kind":"background","startedAt":1786000000000,"sessionId":"aa-full","name":"probe","status":"waiting","waitingFor":"input needed","state":"blocked"}]`
	prevPath := filepath.Join(fixtures, "prev.json")
	curPath := filepath.Join(fixtures, "cur.json")
	if err := os.WriteFile(prevPath, []byte(prev), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(curPath, []byte(cur), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	runner := collect.NewRunner()
	git := collect.NewGitCollector(runner)
	fleet := &collect.Fleet{
		Claude:     collect.NewClaudeCollector(runner),
		Tmux:       collect.NewTmuxCollector(runner),
		Git:        git,
		IncludeAll: true, // mirrors cmdNotify wiring
	}

	run := func(fixture string) string {
		t.Setenv("CLAUDITOR_STUB_FIXTURE", fixture)
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		err := Run(context.Background(), cfg, fleet, Options{Format: "json", StateDir: stateDir})
		os.Stdout = old
		_ = w.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		return buf.String()
	}

	// First run with no state file: establishes the baseline silently
	// (same semantics as --stream startup — no event storm).
	out1 := run(prevPath)
	if strings.TrimSpace(out1) != "" {
		t.Fatalf("first run should be a silent baseline, got: %q", out1)
	}
	// Second run: working -> blocked must emit needs_input.
	out2 := run(curPath)
	var ev Event
	found := false
	for _, line := range strings.Split(strings.TrimSpace(out2), "\n") {
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad JSON line %q: %v", line, err)
		}
		if ev.Type == EventNeedsInput {
			found = true
			if ev.Session.WaitingFor != "input needed" {
				t.Errorf("waitingFor = %q", ev.Session.WaitingFor)
			}
		}
	}
	if !found {
		t.Fatalf("needs_input missing from second run: %q", out2)
	}

	// The stub call log proves the collectors invoked claude with the right argv.
	logData, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "claude agents --all --json") {
		t.Errorf("expected `claude agents --all --json` in call log:\n%s", logData)
	}
}

// TestStubbinsExecutable guards against a checkout losing the +x bit.
func TestStubbinsExecutable(t *testing.T) {
	for _, name := range []string{"claude", "tmux"} {
		p := filepath.Join("..", "..", "test", "stubbin", name)
		if _, err := exec.LookPath(p); err != nil {
			t.Errorf("%s not executable: %v", name, err)
		}
	}
}
