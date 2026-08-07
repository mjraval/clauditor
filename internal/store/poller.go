package store

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/rishi/clauditor/internal/collect"
	"github.com/rishi/clauditor/internal/config"
	"github.com/rishi/clauditor/internal/model"
)

// Poller drives the collectors on their configured cadences and feeds the
// store. Claude polls fastest; tmux and git results are cached between
// their slower ticks so every snapshot is complete.
type Poller struct {
	Fleet *collect.Fleet
	Store *Store
	Cfg   *config.Config
}

// RunOnce performs a single full collection cycle (used by `status` and the
// TUI's in-process fallback).
func (p *Poller) RunOnce(ctx context.Context) *model.Snapshot {
	d := p.Fleet.Collect(ctx)
	now := time.Now()
	if d.ClaudeErr == nil {
		p.Store.MarkCollector("claude", now)
	}
	if d.TmuxErr == nil {
		p.Store.MarkCollector("tmux", now)
	}
	if d.GitErr == nil {
		p.Store.MarkCollector("git", now)
	}
	snap := model.Correlate(model.Inputs{
		Agents: d.Agents, Panes: d.Panes, Procs: d.Procs, Repos: d.Repos, Now: now,
	})
	snap.CollectorAges = p.Store.CollectorAges(now)
	p.Store.Set(snap)
	return snap
}

// Run polls until ctx is done. Cadence: claude every poll.claude_seconds
// (jittered ±20%); tmux and git piggyback on the same loop but only
// re-collect when their own interval has elapsed (their last results are
// reused in between via Fleet's internal caching — see loop body).
func (p *Poller) Run(ctx context.Context) {
	claudeIv := seconds(p.Cfg.Poll.ClaudeSeconds, 5)
	tmuxIv := seconds(p.Cfg.Poll.TmuxSeconds, 10)
	gitIv := seconds(p.Cfg.Poll.GitSeconds, 20)

	var lastTmux, lastGit time.Time
	var cachedPanes []collect.Pane
	var cachedProcs []collect.Proc
	var cachedRepos []collect.RepoInfo

	for {
		now := time.Now()

		agents, claudeErr := p.Fleet.Claude.Agents(ctx, p.Fleet.IncludeAll)
		if claudeErr == nil {
			p.Store.MarkCollector("claude", now)
		}

		if now.Sub(lastTmux) >= tmuxIv {
			panes, tmuxErr := p.Fleet.Tmux.Panes(ctx)
			if tmuxErr == nil {
				p.Store.MarkCollector("tmux", now)
				cachedPanes = panes
				procs, err := p.Fleet.Tmux.Processes(ctx)
				if err == nil {
					cachedProcs = procs
				}
			}
			lastTmux = now
		}

		if now.Sub(lastGit) >= gitIv {
			repoPaths := p.Fleet.Git.DiscoverReposAuto(ctx, p.Fleet.Repos, p.Fleet.WorkspaceDirs, collect.SessionCWDs(agents, cachedPanes))
			var repos []collect.RepoInfo
			ok := true
			for _, rp := range repoPaths {
				wts, err := p.Fleet.Git.Worktrees(ctx, rp)
				if err != nil {
					ok = false
					continue
				}
				repos = append(repos, collect.RepoInfo{Name: baseName(rp), Path: rp, Worktrees: wts})
			}
			if ok || len(repos) > 0 {
				p.Store.MarkCollector("git", now)
			}
			// Keep the previous good repo list when this tick produced
			// nothing but repos were expected — a transient git failure must
			// not dump every session into (loose) for a whole interval
			// (QA correctness finding).
			if len(repos) > 0 || len(repoPaths) == 0 {
				cachedRepos = repos
			}
			lastGit = now
		}

		snap := model.Correlate(model.Inputs{
			Agents: agents, Panes: cachedPanes, Procs: cachedProcs,
			Repos: cachedRepos, Now: now,
		})
		snap.CollectorAges = p.Store.CollectorAges(now)
		p.Store.Set(snap)

		j := time.Duration((rand.Float64()*0.4 - 0.2) * float64(claudeIv))
		select {
		case <-ctx.Done():
			return
		case <-time.After(claudeIv + j):
		}
	}
}

func seconds(v, def int) time.Duration {
	if v <= 0 {
		v = def
	}
	return time.Duration(v) * time.Second
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
