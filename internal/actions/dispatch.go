package actions

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rishi/clauditor/internal/model"
)

// DispatchRequest is POST /api/v1/dispatch's body (SPEC §7.2).
type DispatchRequest struct {
	Target DispatchTarget `json:"target"`
	Prompt string         `json:"prompt"`
	Name   string         `json:"name,omitempty"`
	Model  string         `json:"model,omitempty"`
	Agent  string         `json:"agent,omitempty"`
}

// DispatchTarget selects where the session runs: an explicit cwd, an
// existing repo/worktree, or a new worktree.
type DispatchTarget struct {
	CWD         string       `json:"cwd,omitempty"`
	Repo        string       `json:"repo,omitempty"`
	Worktree    string       `json:"worktree,omitempty"` // worktree path
	NewWorktree *NewWorktree `json:"newWorktree,omitempty"`
}

type NewWorktree struct {
	Branch string `json:"branch"`
	Base   string `json:"base,omitempty"` // ref; empty = HEAD
}

// DispatchResult reports what happened.
type DispatchResult struct {
	Dir          string `json:"dir"`
	ShortID      string `json:"shortId,omitempty"`
	CreatedWT    string `json:"createdWorktree,omitempty"`
	RawOutput    string `json:"rawOutput,omitempty"`
}

// Dispatch resolves the target directory (creating a worktree if asked),
// then runs `claude --bg [--name N] [--model M] [--agent A] <prompt>` with
// Dir set (SPEC §7.2). Permission-bypass flags are rejected wherever they
// hide.
func (a *Actions) Dispatch(ctx context.Context, snap *model.Snapshot, req DispatchRequest) (*DispatchResult, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errf("bad_request", "prompt is required")
	}
	if err := checkDenied(req.Prompt, req.Name, req.Model, req.Agent); err != nil {
		return nil, err
	}
	// Name/model/agent become argv values; refuse anything flag-shaped so
	// they can't be smuggled as extra options.
	for _, v := range []string{req.Name, req.Model, req.Agent} {
		if strings.HasPrefix(v, "-") {
			return nil, errf("bad_request", "name/model/agent must not start with '-'")
		}
	}

	res := &DispatchResult{}
	dir, err := a.resolveTarget(ctx, snap, req.Target, res)
	if err != nil {
		return nil, err
	}
	res.Dir = dir

	args := []string{"--bg"}
	if req.Name != "" {
		args = append(args, "--name", req.Name)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Agent != "" {
		args = append(args, "--agent", req.Agent)
	}
	args = append(args, req.Prompt)

	out, err := a.run(ctx, dir, a.ClaudeBin, args...)
	if err != nil {
		return res, err
	}
	res.RawOutput = string(out)
	res.ShortID = parseShortID(string(out))
	return res, nil
}

// backgroundedRe extracts the short id from `backgrounded · 4ee88fb6 [· name]`.
var backgroundedRe = regexp.MustCompile(`backgrounded\s*·\s*([0-9a-f]{6,})`)

func parseShortID(out string) string {
	if m := backgroundedRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func (a *Actions) resolveTarget(ctx context.Context, snap *model.Snapshot, t DispatchTarget, res *DispatchResult) (string, error) {
	switch {
	case t.CWD != "":
		st, err := os.Stat(t.CWD)
		if err != nil || !st.IsDir() {
			return "", errf("bad_target", "cwd %q is not a directory", t.CWD)
		}
		return t.CWD, nil

	case t.Repo != "" && t.NewWorktree != nil:
		repo := findRepo(snap, t.Repo)
		if repo == nil {
			return "", errf("bad_target", "unknown repo %q", t.Repo)
		}
		return a.createWorktree(ctx, repo.Path, t.NewWorktree, res)

	case t.Repo != "" && t.Worktree != "":
		repo := findRepo(snap, t.Repo)
		if repo == nil {
			return "", errf("bad_target", "unknown repo %q", t.Repo)
		}
		for _, wt := range repo.Worktrees {
			if wt.Path == t.Worktree {
				return wt.Path, nil
			}
		}
		return "", errf("bad_target", "worktree %q not found in repo %q", t.Worktree, t.Repo)

	case t.Repo != "":
		repo := findRepo(snap, t.Repo)
		if repo == nil {
			return "", errf("bad_target", "unknown repo %q", t.Repo)
		}
		return repo.Path, nil

	default:
		return "", errf("bad_target", "target requires cwd, repo, repo+worktree, or repo+newWorktree")
	}
}

// createWorktree follows the dmux-derived recipe (docs/RESEARCH.md §2.3):
// validate → prune → verify base → path triage → branch-exists probe →
// worktree add. Dispatching inside a linked worktree makes Claude Code skip
// its own bg isolation (Appendix A).
func (a *Actions) createWorktree(ctx context.Context, repoPath string, nw *NewWorktree, res *DispatchResult) (string, error) {
	if !ValidBranchName(nw.Branch) {
		return "", errf("bad_branch", "invalid branch name %q", nw.Branch)
	}
	if nw.Base != "" && !ValidBranchName(strings.TrimPrefix(nw.Base, "origin/")) {
		return "", errf("bad_branch", "invalid base ref %q", nw.Base)
	}

	base := a.WorktreeBase
	if base == "" {
		base = filepath.Join(filepath.Dir(repoPath), filepath.Base(repoPath)+"-worktrees")
	}
	dir := filepath.Join(base, SlugForBranch(nw.Branch))

	_, _ = a.Runner.Run(ctx, repoPath, "git", "worktree", "prune") // idempotent, failures fine

	if nw.Base != "" {
		if _, err := a.Runner.Run(ctx, repoPath, "git", "rev-parse", "--verify", "--end-of-options", nw.Base); err != nil {
			return "", errf("bad_base", "base ref %q does not resolve", nw.Base)
		}
	}

	// Path triage: exists with .git → idempotent success; exists without → error.
	if st, err := os.Stat(dir); err == nil {
		if !st.IsDir() {
			return "", errf("bad_target", "%s exists and is not a directory", dir)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			res.CreatedWT = "" // pre-existing
			return dir, nil
		}
		return "", errf("bad_target", "%s exists but is not a worktree", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", errf("exec_failed", "mkdir: %v", err)
	}

	// Branch-exists probe selects the argv shape.
	_, probeErr := a.Runner.Run(ctx, repoPath, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+nw.Branch)
	var args []string
	if probeErr == nil {
		args = []string{"worktree", "add", dir, nw.Branch}
	} else {
		args = []string{"worktree", "add", dir, "-b", nw.Branch}
		if nw.Base != "" {
			args = append(args, nw.Base)
		}
	}
	if _, err := a.run(ctx, repoPath, "git", args...); err != nil {
		return "", err
	}
	res.CreatedWT = dir
	return dir, nil
}

func findRepo(snap *model.Snapshot, name string) *model.Repo {
	if snap == nil {
		return nil
	}
	for _, r := range snap.Repos {
		if r.Name == name || r.Path == name {
			return r
		}
	}
	return nil
}
