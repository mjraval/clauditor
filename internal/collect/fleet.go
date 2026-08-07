package collect

import (
	"context"
	"log/slog"
	"path/filepath"
)

// Fleet runs all three collectors once and returns their raw outputs.
// Individual collector failures degrade (logged, empty set) rather than
// failing the cycle — except the claude collector, whose failure is the
// caller's signal that the supervisor surface is unavailable.
type Fleet struct {
	Claude *ClaudeCollector
	Tmux   *TmuxCollector
	Git    *GitCollector

	Repos         []string
	WorkspaceDirs []string
	IncludeAll    bool // pass --all to claude agents
}

// FleetData is one cycle's raw output.
type FleetData struct {
	Agents    []AgentEntry
	Panes     []Pane
	Procs     []Proc
	Repos     []RepoInfo
	ClaudeErr error
	TmuxErr   error
	GitErr    error
}

// Collect runs one full cycle.
func (f *Fleet) Collect(ctx context.Context) FleetData {
	var d FleetData

	d.Agents, d.ClaudeErr = f.Claude.Agents(ctx, f.IncludeAll)
	if d.ClaudeErr != nil {
		slog.Warn("claude collector failed", "err", d.ClaudeErr)
	}

	d.Panes, d.TmuxErr = f.Tmux.Panes(ctx)
	if d.TmuxErr != nil {
		slog.Warn("tmux collector failed", "err", d.TmuxErr)
	}
	if len(d.Panes) > 0 || len(d.Agents) > 0 {
		var err error
		d.Procs, err = f.Tmux.Processes(ctx)
		if err != nil {
			slog.Warn("ps failed", "err", err)
		}
	}

	repoPaths := f.Git.DiscoverRepos(ctx, f.Repos, f.WorkspaceDirs)
	for _, rp := range repoPaths {
		wts, err := f.Git.Worktrees(ctx, rp)
		if err != nil {
			slog.Warn("worktree list failed", "repo", rp, "err", err)
			d.GitErr = err
			continue
		}
		d.Repos = append(d.Repos, RepoInfo{Name: filepath.Base(rp), Path: rp, Worktrees: wts})
	}
	return d
}
