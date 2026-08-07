package collect

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RepoInfo is a discovered repository with its worktrees.
type RepoInfo struct {
	Name      string
	Path      string
	Worktrees []WorktreeInfo
}

// WorktreeInfo is one entry of `git worktree list --porcelain`.
type WorktreeInfo struct {
	Path      string
	Head      string
	Branch    string // short name, e.g. "feat/kms" ("" when detached)
	Dirty     string // "true" | "false" | "unknown"
	ManagedBy string // model.ManagedByUser | model.ManagedByClaudeCode
	Ahead     int    // commits ahead of upstream (git.ahead_behind only)
	Behind    int    // commits behind upstream (git.ahead_behind only)
	HasCounts bool   // Ahead/Behind were computed (upstream exists, no error)
}

// GitCollector discovers repos and their worktrees.
type GitCollector struct {
	Runner      Runner
	DirtyCheck  bool
	AheadBehind bool
}

func NewGitCollector(r Runner) *GitCollector {
	return &GitCollector{Runner: r, DirtyCheck: true}
}

// DiscoverRepos unions explicit repo paths with a depth-2 scan of workspace
// dirs for `.git` (dir or file — a .git file means linked worktree; those
// resolve to their main repo and dedupe).
func (c *GitCollector) DiscoverRepos(ctx context.Context, repos, workspaceDirs []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, r := range repos {
		if st, err := os.Stat(filepath.Join(r, ".git")); err == nil {
			if st.IsDir() {
				add(r)
			} else if main := c.resolveMainRepo(ctx, r); main != "" {
				add(main)
			}
		}
	}
	for _, ws := range workspaceDirs {
		for _, cand := range scanForGit(ws, 2) {
			if st, err := os.Stat(filepath.Join(cand, ".git")); err == nil {
				if st.IsDir() {
					add(cand)
				} else if main := c.resolveMainRepo(ctx, cand); main != "" {
					add(main)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// DiscoverReposAuto returns the repo roots to inspect this cycle. When either
// explicit repos or workspace dirs are configured it defers to DiscoverRepos
// (the configured behaviour). When BOTH are empty — the zero-config case — it
// derives repo roots from the live sessions' cwds: each distinct cwd is
// resolved to its git toplevel (`git -C <cwd> rev-parse --show-toplevel`;
// failure = not a repo, tolerated), then linked worktrees are mapped to their
// main repo via resolveMainRepo, and the result is deduped. This is what makes
// `clauditor` correlate sessions to repos with no config at all.
func (c *GitCollector) DiscoverReposAuto(ctx context.Context, repos, workspaceDirs, agentCWDs []string) []string {
	if len(repos) > 0 || len(workspaceDirs) > 0 {
		return c.DiscoverRepos(ctx, repos, workspaceDirs)
	}
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = filepath.Clean(p)
		if p == "" || p == "." || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	cwdSeen := map[string]bool{}
	for _, cwd := range agentCWDs {
		cwd = filepath.Clean(cwd)
		if cwd == "" || cwd == "." || cwdSeen[cwd] {
			continue
		}
		cwdSeen[cwd] = true
		top := c.gitToplevel(ctx, cwd)
		if top == "" {
			continue // not a git repo — tolerate
		}
		if main := c.resolveMainRepo(ctx, top); main != "" {
			add(main) // maps a linked worktree to its main repo (and is a no-op for a main checkout)
		} else {
			add(top)
		}
	}
	sort.Strings(out)
	return out
}

// gitToplevel returns the working-tree root of dir, or "" when dir is not
// inside a git repository (or git fails). Doubles as the is-this-a-repo probe.
func (c *GitCollector) gitToplevel(ctx context.Context, dir string) string {
	out, err := c.Runner.Run(ctx, dir, "git", "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// scanForGit returns directories under root (inclusive) up to maxDepth that
// contain a .git entry.
func scanForGit(root string, maxDepth int) []string {
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			out = append(out, dir)
			return // don't descend into repos
		}
		if depth >= maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				walk(filepath.Join(dir, e.Name()), depth+1)
			}
		}
	}
	walk(filepath.Clean(root), 0)
	return out
}

// resolveMainRepo maps a linked worktree to its main repository root.
func (c *GitCollector) resolveMainRepo(ctx context.Context, wt string) string {
	out, err := c.Runner.Run(ctx, wt, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return ""
	}
	// common dir is <main>/.git
	return filepath.Dir(common)
}

// Worktrees lists a repo's worktrees and (optionally) their dirty state.
func (c *GitCollector) Worktrees(ctx context.Context, repo string) ([]WorktreeInfo, error) {
	out, err := c.Runner.Run(ctx, repo, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	wts := ParseWorktreePorcelain(out)
	for i := range wts {
		if strings.Contains(wts[i].Path, string(filepath.Separator)+".claude"+string(filepath.Separator)+"worktrees"+string(filepath.Separator)) {
			wts[i].ManagedBy = "claude-code"
		} else {
			wts[i].ManagedBy = "user"
		}
		if c.DirtyCheck {
			wts[i].Dirty = c.dirty(ctx, wts[i].Path)
		} else {
			wts[i].Dirty = "unknown"
		}
		if c.AheadBehind {
			wts[i].Ahead, wts[i].Behind, wts[i].HasCounts = c.aheadBehind(ctx, wts[i].Path)
		}
	}
	return wts, nil
}

// aheadBehind runs `git rev-list --left-right --count @{upstream}...HEAD`
// (SPEC §5.3). Output is "<behind>\t<ahead>" because upstream is the left
// side. No upstream (or any error) yields ok=false, never an error.
func (c *GitCollector) aheadBehind(ctx context.Context, wt string) (ahead, behind int, ok bool) {
	dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := c.Runner.Run(dctx, wt, "git", "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		return 0, 0, false
	}
	b, err1 := strconv.Atoi(fields[0])
	a, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return a, b, true
}

func (c *GitCollector) dirty(ctx context.Context, wt string) string {
	dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := c.Runner.Run(dctx, wt, "git", "--no-optional-locks", "status", "--porcelain", "-unormal")
	if err != nil {
		return "unknown"
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return "false"
	}
	return "true"
}

// ParseWorktreePorcelain decodes `git worktree list --porcelain` output.
func ParseWorktreePorcelain(data []byte) []WorktreeInfo {
	var out []WorktreeInfo
	var cur *WorktreeInfo
	flush := func() {
		if cur != nil && cur.Path != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &WorktreeInfo{Path: strings.TrimPrefix(line, "worktree "), Dirty: "unknown"}
		case cur == nil:
			continue
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return out
}
