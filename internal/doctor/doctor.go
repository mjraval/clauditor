// Package doctor implements `clauditor doctor`'s environment checks
// (SPEC §12): claude/tmux/git presence and version thresholds, the
// supervisor's reachability, config validity, repo/workspace-dir sanity,
// and Access JWKS reachability. Every check is a small, independently
// testable function producing a Check; RunAll is the only thing callers
// need.
//
// Reconciliation note: SPEC §12 lists "config parses" as one of doctor's
// checks, but RunAll here takes an already-loaded *config.Config (so a
// Runner-based check suite has something to check repos/workspace dirs
// against). The config-parse attempt itself happens one layer up, in
// cmd/clauditor/doctor.go, where config.Load's error (if any) becomes its
// own Check before RunAll runs — RunAll always receives a non-nil config
// (falling back to config.Default() when loading failed) so the remaining
// checks still produce meaningful output instead of skipping en masse.
package doctor

import (
	"context"
	"net/http"
	"time"

	"github.com/mjraval/clauditor/internal/collect"
	"github.com/mjraval/clauditor/internal/config"
)

// Status is one of the four check outcomes.
type Status string

const (
	PASS Status = "PASS"
	WARN Status = "WARN"
	FAIL Status = "FAIL"
	SKIP Status = "SKIP"
)

// Check is one row of the doctor report.
type Check struct {
	Name   string
	Status Status
	Detail string
}

// AnyFail reports whether any check FAILed — the signal cmdDoctor uses to
// decide its exit code (non-nil error → main.go exits 1).
func AnyFail(checks []Check) bool {
	for _, c := range checks {
		if c.Status == FAIL {
			return true
		}
	}
	return false
}

// RunAll runs every environment check and returns the full report in a
// fixed, stable order. Runner-based checks each get their own bounded
// context (collect.DefaultTimeout, or the Access check's own 5s HTTP
// timeout) so one hanging external command can't stall the rest.
func RunAll(ctx context.Context, cfg *config.Config, runner collect.Runner) []Check {
	var out []Check
	out = append(out, checkClaudeVersion(ctx, runner))
	out = append(out, checkAgentsJSON(ctx, runner))
	out = append(out, checkDaemon(ctx, runner))
	out = append(out, checkTmuxVersion(ctx, runner))
	out = append(out, checkGitVersion(ctx, runner))
	out = append(out, checkRepos(cfg)...)
	out = append(out, checkWorkspaceDirs(cfg)...)
	out = append(out, CheckAccessJWKS(cfg.Access.TeamDomain, &http.Client{Timeout: 5 * time.Second}))
	return out
}

// withTimeout narrows ctx to collect.DefaultTimeout unless it already has a
// tighter deadline.
func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, collect.DefaultTimeout)
}
