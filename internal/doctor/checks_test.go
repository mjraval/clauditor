package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mjraval/clauditor/internal/collect"
	"github.com/mjraval/clauditor/internal/config"
)

// fakeRunner substitutes collect.Runner in unit tests that don't need
// test/stubbin (e.g. asserting exact classification of a version string
// or an ExitError shape that the real stub can't fabricate, per the
// dispatch instructions on the daemon "not running" case).
type fakeRunner struct {
	fn func(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

func (f fakeRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	return f.fn(ctx, dir, name, args...)
}

func staticOut(out string, err error) fakeRunner {
	return fakeRunner{fn: func(context.Context, string, string, ...string) ([]byte, error) {
		return []byte(out), err
	}}
}

func TestCheckClaudeVersion(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		err    error
		status Status
	}{
		{"below min", "2.1.100 (Claude Code)", nil, FAIL},
		{"at min, below degraded threshold", "2.1.139 (Claude Code)", nil, WARN},
		{"between thresholds", "2.1.200 (Claude Code)", nil, WARN},
		{"at degraded threshold", "2.1.212 (Claude Code)", nil, PASS},
		{"above upper", "2.1.223 (Claude Code)", nil, PASS},
		{"unparseable", "no version here", nil, FAIL},
		{"exec error", "", errCmdNotFound, FAIL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := checkClaudeVersion(context.Background(), staticOut(tc.out, tc.err))
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s (detail: %s)", got.Status, tc.status, got.Detail)
			}
		})
	}
}

func TestCheckTmuxVersion(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		status Status
	}{
		{"below min", "tmux 2.9", FAIL},
		{"at min", "tmux 3.0", PASS},
		{"above min", "tmux 3.4", PASS},
		{"unparseable", "garbage", FAIL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := checkTmuxVersion(context.Background(), staticOut(tc.out, nil))
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s (detail: %s)", got.Status, tc.status, got.Detail)
			}
		})
	}
	t.Run("exec error", func(t *testing.T) {
		got := checkTmuxVersion(context.Background(), staticOut("", errCmdNotFound))
		if got.Status != FAIL {
			t.Fatalf("status = %s, want FAIL", got.Status)
		}
	})
}

func TestCheckGitVersion(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		status Status
	}{
		{"below min", "git version 2.19.0", FAIL},
		{"at min", "git version 2.20.0", PASS},
		{"above min", "git version 2.43.0", PASS},
		{"unparseable", "garbage", FAIL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := checkGitVersion(context.Background(), staticOut(tc.out, nil))
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s (detail: %s)", got.Status, tc.status, got.Detail)
			}
		})
	}
}

var errCmdNotFound = &execNotFoundErr{}

type execNotFoundErr struct{}

func (e *execNotFoundErr) Error() string { return "exec: \"x\": executable file not found in $PATH" }

func TestCheckAgentsJSON(t *testing.T) {
	t.Run("exec error", func(t *testing.T) {
		got := checkAgentsJSON(context.Background(), staticOut("", errCmdNotFound))
		if got.Status != FAIL {
			t.Fatalf("status = %s, want FAIL", got.Status)
		}
	})
	t.Run("unparseable json", func(t *testing.T) {
		got := checkAgentsJSON(context.Background(), staticOut("not json", nil))
		if got.Status != FAIL {
			t.Fatalf("status = %s, want FAIL", got.Status)
		}
	})
	t.Run("empty array parses", func(t *testing.T) {
		got := checkAgentsJSON(context.Background(), staticOut("[]", nil))
		if got.Status != PASS {
			t.Fatalf("status = %s, want PASS (detail: %s)", got.Status, got.Detail)
		}
	})
	t.Run("real entry parses", func(t *testing.T) {
		out := `[{"pid":1,"id":"aa","cwd":"/tmp/x","kind":"background","startedAt":1786000000000,"sessionId":"aa-full","name":"probe","status":"busy","state":"working"}]`
		got := checkAgentsJSON(context.Background(), staticOut(out, nil))
		if got.Status != PASS {
			t.Fatalf("status = %s, want PASS (detail: %s)", got.Status, got.Detail)
		}
	})
}

// TestCheckDaemon_Classification is the required unit test that fabricates
// both the "not running" (WARN) and generic-error (FAIL) cases via a fake
// Runner — the real test/stubbin `claude daemon` always exits 0, so it
// cannot exercise either non-zero path.
func TestCheckDaemon_Classification(t *testing.T) {
	t.Run("exit 0 is PASS", func(t *testing.T) {
		got := checkDaemon(context.Background(), staticOut("pid: 12345", nil))
		if got.Status != PASS {
			t.Fatalf("status = %s, want PASS", got.Status)
		}
	})
	t.Run("not-running stdout is WARN", func(t *testing.T) {
		r := staticOut("daemon is not running", &collect.ExitError{Cmd: "claude", Code: 1, Stderr: ""})
		got := checkDaemon(context.Background(), r)
		if got.Status != WARN {
			t.Fatalf("status = %s, want WARN (detail: %s)", got.Status, got.Detail)
		}
	})
	t.Run("not-running stderr, case-insensitive, is WARN", func(t *testing.T) {
		r := staticOut("", &collect.ExitError{Cmd: "claude", Code: 1, Stderr: "Supervisor NOT RUNNING"})
		got := checkDaemon(context.Background(), r)
		if got.Status != WARN {
			t.Fatalf("status = %s, want WARN (detail: %s)", got.Status, got.Detail)
		}
	})
	t.Run("other exit error is FAIL", func(t *testing.T) {
		r := staticOut("", &collect.ExitError{Cmd: "claude", Code: 1, Stderr: "socket permission denied"})
		got := checkDaemon(context.Background(), r)
		if got.Status != FAIL {
			t.Fatalf("status = %s, want FAIL (detail: %s)", got.Status, got.Detail)
		}
	})
	t.Run("non-exec-error is FAIL", func(t *testing.T) {
		got := checkDaemon(context.Background(), staticOut("", errCmdNotFound))
		if got.Status != FAIL {
			t.Fatalf("status = %s, want FAIL", got.Status)
		}
	})
}

func TestCheckRepos(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "realrepo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	notRepo := filepath.Join(dir, "notarepo")
	if err := os.MkdirAll(notRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "does-not-exist")

	cfg := &config.Config{Repos: []string{repo, notRepo, missing}}
	checks := checkRepos(cfg)
	if len(checks) != 3 {
		t.Fatalf("got %d checks, want 3", len(checks))
	}
	if checks[0].Status != PASS {
		t.Errorf("real repo: status = %s, want PASS (detail: %s)", checks[0].Status, checks[0].Detail)
	}
	if checks[1].Status != FAIL {
		t.Errorf("non-repo dir: status = %s, want FAIL", checks[1].Status)
	}
	if checks[2].Status != FAIL {
		t.Errorf("missing path: status = %s, want FAIL", checks[2].Status)
	}
}

func TestCheckWorkspaceDirs(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope")
	cfg := &config.Config{WorkspaceDirs: []string{dir, missing}}
	checks := checkWorkspaceDirs(cfg)
	if len(checks) != 2 {
		t.Fatalf("got %d checks, want 2", len(checks))
	}
	if checks[0].Status != PASS {
		t.Errorf("existing dir: status = %s, want PASS", checks[0].Status)
	}
	if checks[1].Status != FAIL {
		t.Errorf("missing dir: status = %s, want FAIL", checks[1].Status)
	}
}
