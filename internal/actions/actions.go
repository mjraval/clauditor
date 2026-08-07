// Package actions implements clauditor's mutating operations: dispatch,
// stop, respawn, open-in-tmux, and the experimental reply. Every external
// command is argv-exec'd (never a shell string) with a context timeout.
package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/rishi/clauditor/internal/collect"
)

// Actions wires the runner and config the handlers need.
type Actions struct {
	Runner       collect.Runner
	ClaudeBin    string // default "claude"
	TmuxBin      string // default "tmux"
	WorktreeBase string // default <repo>/../<repo>-worktrees
}

func New(r collect.Runner) *Actions {
	return &Actions{Runner: r, ClaudeBin: "claude", TmuxBin: "tmux"}
}

// ActionError carries an HTTP-mappable code.
type ActionError struct {
	Code    string // e.g. "bad_target", "forbidden_flag", "exec_failed"
	Message string
}

func (e *ActionError) Error() string { return e.Code + ": " + e.Message }

func errf(code, format string, a ...any) *ActionError {
	return &ActionError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// deniedFlagRe rejects permission-bypass flags anywhere in prompt/name/model/
// agent inputs (SPEC §9: never invoke claude with permission bypass; the
// deny-list covers the agents-subcommand spellings found in Phase 0 too).
var deniedFlagRe = regexp.MustCompile(`--dangerously-skip-permissions|--allow-dangerously-skip-permissions|--permission-mode[=\s]+bypassPermissions|bypassPermissions`)

// checkDenied returns an error when any input smuggles a bypass flag.
func checkDenied(inputs ...string) error {
	for _, in := range inputs {
		if deniedFlagRe.MatchString(in) {
			return errf("forbidden_flag", "permission-bypass flags are never allowed")
		}
	}
	return nil
}

// validSessionIDRe matches claude's short ids and full session UUIDs.
// Defense in depth: ids come from `claude agents --json` (trusted today),
// but they end up inside tmux window-command strings — never let anything
// shell-metacharacter-shaped through (QA security finding, 2026-08-07).
var validSessionIDRe = regexp.MustCompile(`^[0-9a-fA-F-]{4,64}$`)

// ValidSessionID reports whether id is safe to embed in an attach command.
func ValidSessionID(id string) bool { return validSessionIDRe.MatchString(id) }

// validBranchRe is the dmux-derived pure predicate (see docs/RESEARCH.md);
// git check-ref-format semantics approximated: safe charset, no traversal.
var validBranchRe = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)

// ValidBranchName reports whether name is safe to hand to git.
func ValidBranchName(name string) bool {
	if name == "" || len(name) > 200 || !validBranchRe.MatchString(name) {
		return false
	}
	if strings.Contains(name, "..") || strings.Contains(name, "//") || strings.Contains(name, "@{") {
		return false
	}
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") ||
		strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".lock") {
			return false
		}
	}
	return true
}

// SlugForBranch converts a branch name to a filesystem slug (dmux-style:
// slashes and unsafe chars collapse to '-').
func SlugForBranch(branch string) string {
	s := strings.ToLower(branch)
	s = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-.")
	}
	if s == "" {
		s = "worktree"
	}
	return s
}

// run wraps Runner.Run mapping failures to ActionError. The error message
// deliberately omits argv: dispatch prompts and reply text ride in args and
// may contain secrets (SPEC §9) — the full command line is logged at debug
// only, never returned to HTTP clients.
func (a *Actions) run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	out, err := a.Runner.Run(ctx, dir, name, args...)
	if err != nil {
		slog.Debug("action exec failed", "cmd", name, "args", args, "dir", dir, "err", err)
		return out, errf("exec_failed", "%s %s failed: %v", name, firstArg(args), summarizeExecErr(err))
	}
	return out, nil
}

// firstArg names the subcommand (never user content — prompts are trailing).
func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// summarizeExecErr keeps exit codes and stderr (diagnostic, produced by the
// tool) but the caller has already dropped argv (may contain user secrets).
func summarizeExecErr(err error) string {
	var xe *collect.ExitError
	if errors.As(err, &xe) {
		return fmt.Sprintf("exit %d: %s", xe.Code, strings.TrimSpace(xe.Stderr))
	}
	return err.Error()
}
