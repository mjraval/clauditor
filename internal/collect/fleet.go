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
	Claude   *ClaudeCollector
	Tmux     *TmuxCollector
	Git      *GitCollector
	Sessions *SessionsCollector // presence-registry enrichment (docs/MESSAGING.md §4.1)

	Repos         []string
	WorkspaceDirs []string
	IncludeAll    bool // pass --all to claude agents
}

// FleetData is one cycle's raw output.
type FleetData struct {
	Agents      []AgentEntry
	Panes       []Pane
	Procs       []Proc
	Repos       []RepoInfo
	SessionRegs []SessionReg // presence registry, keyed by SessionID for enrichment
	ClaudeErr   error
	TmuxErr     error
	GitErr      error
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

	// Presence-registry enrichment (docs/MESSAGING.md §4.1): cheap, local,
	// read-only. Never a state authority, so a failure here only drops
	// enrichment for the cycle — it never fails the fleet collection.
	sessions := f.Sessions
	if sessions == nil {
		sessions = NewSessionsCollector()
	}
	if regs, err := sessions.Registry(); err != nil {
		slog.Warn("sessions registry read failed", "err", err)
	} else {
		d.SessionRegs = regs
	}
	if len(d.Panes) > 0 || len(d.Agents) > 0 {
		var err error
		d.Procs, err = f.Tmux.Processes(ctx)
		if err != nil {
			slog.Warn("ps failed", "err", err)
		}
	}

	repoPaths := f.Git.DiscoverReposAuto(ctx, f.Repos, f.WorkspaceDirs, SessionCWDs(d.Agents, d.Panes))
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

// SessionCWDs gathers the cwds of every live session this cycle — both
// supervisor agents and claude-running tmux panes — for zero-config repo
// discovery (see GitCollector.DiscoverReposAuto).
func SessionCWDs(agents []AgentEntry, panes []Pane) []string {
	out := make([]string, 0, len(agents)+len(panes))
	for _, a := range agents {
		if a.CWD != "" {
			out = append(out, a.CWD)
		}
	}
	for _, p := range panes {
		if p.CurrentPath != "" {
			out = append(out, p.CurrentPath)
		}
	}
	return out
}
