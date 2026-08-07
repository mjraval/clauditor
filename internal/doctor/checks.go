package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rishi/clauditor/internal/collect"
	"github.com/rishi/clauditor/internal/config"
)

// Version thresholds (SPEC §12, Appendix A).
var (
	claudeMin      = semver{2, 1, 139}
	claudeDegraded = semver{2, 1, 212} // WARN below this, PASS at/above
	tmuxMin        = semver{3, 0, 0}
	gitMin         = semver{2, 20, 0}
)

// checkClaudeVersion runs `claude --version` and classifies it against the
// two documented thresholds.
func checkClaudeVersion(ctx context.Context, r collect.Runner) Check {
	const name = "claude --version"
	cctx, cancel := withTimeout(ctx)
	defer cancel()
	out, err := r.Run(cctx, "", "claude", "--version")
	if err != nil {
		return Check{name, FAIL, fmt.Sprintf("claude not runnable: %v", err)}
	}
	v, ok := parseSemver(string(out))
	if !ok {
		return Check{name, FAIL, fmt.Sprintf("could not parse a version from %q", strings.TrimSpace(string(out)))}
	}
	switch {
	case v.less(claudeMin):
		return Check{name, FAIL, fmt.Sprintf("%s < %s — claude agents research preview requires v2.1.139+", v, claudeMin)}
	case v.less(claudeDegraded):
		return Check{name, WARN, fmt.Sprintf("%s — /fork-to-bg, /resume picker degraded below v2.1.212", v)}
	default:
		return Check{name, PASS, v.String()}
	}
}

// checkAgentsJSON confirms `claude agents --json` both executes and parses
// via the real collector parser (collect.ParseAgentsJSON), so doctor is
// actually exercising the same code path the collectors use.
func checkAgentsJSON(ctx context.Context, r collect.Runner) Check {
	const name = "claude agents --json"
	cctx, cancel := withTimeout(ctx)
	defer cancel()
	out, err := r.Run(cctx, "", "claude", "agents", "--json")
	if err != nil {
		return Check{name, FAIL, fmt.Sprintf("exec failed: %v", err)}
	}
	entries, err := collect.ParseAgentsJSON(out)
	if err != nil {
		return Check{name, FAIL, fmt.Sprintf("parse failed: %v", err)}
	}
	return Check{name, PASS, fmt.Sprintf("%d entries parsed", len(entries))}
}

// checkDaemon runs `claude daemon status`. Exit 0 is PASS. A non-zero exit
// whose captured stdout/stderr mentions "not running" is a normal,
// expected condition (the supervisor starts on demand) and is WARN, not
// FAIL. Any other non-zero exit is FAIL.
func checkDaemon(ctx context.Context, r collect.Runner) Check {
	const name = "claude daemon status"
	cctx, cancel := withTimeout(ctx)
	defer cancel()
	out, err := r.Run(cctx, "", "claude", "daemon", "status")
	if err == nil {
		return Check{name, PASS, strings.TrimSpace(string(out))}
	}
	var ee *collect.ExitError
	if errors.As(err, &ee) {
		combined := strings.ToLower(string(out) + " " + ee.Stderr)
		if strings.Contains(combined, "not running") {
			return Check{name, WARN, "supervisor not running — starts on demand"}
		}
		return Check{name, FAIL, fmt.Sprintf("exit %d: %s", ee.Code, ee.Stderr)}
	}
	return Check{name, FAIL, err.Error()}
}

// checkTmuxVersion runs `tmux -V` and requires >= 3.0.
func checkTmuxVersion(ctx context.Context, r collect.Runner) Check {
	const name = "tmux -V"
	cctx, cancel := withTimeout(ctx)
	defer cancel()
	out, err := r.Run(cctx, "", "tmux", "-V")
	if err != nil {
		return Check{name, FAIL, fmt.Sprintf("tmux not runnable: %v", err)}
	}
	v, ok := parseSemver(string(out))
	if !ok {
		return Check{name, FAIL, fmt.Sprintf("could not parse a version from %q", strings.TrimSpace(string(out)))}
	}
	if v.less(tmuxMin) {
		return Check{name, FAIL, fmt.Sprintf("%s < %s", v, tmuxMin)}
	}
	return Check{name, PASS, v.String()}
}

// checkGitVersion runs `git --version` and requires >= 2.20. Unlike
// claude/tmux, git is never stubbed in tests (SPEC intent: git/repo checks
// run against the real git on PATH), but the function itself still takes a
// Runner so it is unit-testable with a fake.
func checkGitVersion(ctx context.Context, r collect.Runner) Check {
	const name = "git --version"
	cctx, cancel := withTimeout(ctx)
	defer cancel()
	out, err := r.Run(cctx, "", "git", "--version")
	if err != nil {
		return Check{name, FAIL, fmt.Sprintf("git not runnable: %v", err)}
	}
	v, ok := parseSemver(string(out))
	if !ok {
		return Check{name, FAIL, fmt.Sprintf("could not parse a version from %q", strings.TrimSpace(string(out)))}
	}
	if v.less(gitMin) {
		return Check{name, FAIL, fmt.Sprintf("%s < %s", v, gitMin)}
	}
	return Check{name, PASS, v.String()}
}

// checkRepos emits one Check per configured `repos` entry: the path must
// exist and contain a .git entry (dir or file — mirrors the simple stat
// check in internal/collect/gitwt.go, not its worktree-resolution logic).
func checkRepos(cfg *config.Config) []Check {
	out := make([]Check, 0, len(cfg.Repos))
	for _, path := range cfg.Repos {
		out = append(out, checkRepoPath(path))
	}
	return out
}

func checkRepoPath(path string) Check {
	name := "repo: " + path
	st, err := os.Stat(path)
	if err != nil {
		return Check{name, FAIL, fmt.Sprintf("path error: %v", err)}
	}
	if !st.IsDir() {
		return Check{name, FAIL, "exists but is not a directory"}
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		return Check{name, FAIL, "no .git entry — not a git repo"}
	}
	return Check{name, PASS, "exists, is a git repo"}
}

// checkWorkspaceDirs emits one Check per configured `workspace_dirs`
// entry. These are scanned for repos, not repos themselves, so they only
// need to exist.
func checkWorkspaceDirs(cfg *config.Config) []Check {
	out := make([]Check, 0, len(cfg.WorkspaceDirs))
	for _, dir := range cfg.WorkspaceDirs {
		name := "workspace dir: " + dir
		st, err := os.Stat(dir)
		switch {
		case err != nil:
			out = append(out, Check{name, FAIL, fmt.Sprintf("path error: %v", err)})
		case !st.IsDir():
			out = append(out, Check{name, FAIL, "exists but is not a directory"})
		default:
			out = append(out, Check{name, PASS, "exists"})
		}
	}
	return out
}
