package model

import (
	"testing"
	"time"

	"github.com/rishi/clauditor/internal/collect"
)

// The M2 acceptance scenario (SPEC §7.1): 2 repos, 3 worktrees, supervisor
// sessions in all five states, one tmux-interactive session, one loose
// session, one dedupe case.
func fixtureInputs() Inputs {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	ms := func(minAgo int) int64 { return now.Add(-time.Duration(minAgo) * time.Minute).UnixMilli() }
	return Inputs{
		Now: now,
		Repos: []collect.RepoInfo{
			{Name: "alpha", Path: "/repos/alpha", Worktrees: []collect.WorktreeInfo{
				{Path: "/repos/alpha", Branch: "main", Head: "aaa", Dirty: "false", ManagedBy: "user"},
				{Path: "/repos/alpha-worktrees/feat-kms", Branch: "feat/kms", Head: "bbb", Dirty: "true", ManagedBy: "user"},
			}},
			{Name: "beta", Path: "/repos/beta", Worktrees: []collect.WorktreeInfo{
				{Path: "/repos/beta", Branch: "main", Head: "ccc", Dirty: "unknown", ManagedBy: "user"},
			}},
		},
		Agents: []collect.AgentEntry{
			// five bg states
			{ID: "b1", SessionID: "s-working", Kind: "background", State: "working", Status: "busy", CWD: "/repos/alpha-worktrees/feat-kms", PID: 501, StartedAt: ms(10), Name: "kms rotation"},
			{ID: "b2", SessionID: "s-blocked", Kind: "background", State: "blocked", Status: "waiting", WaitingFor: "permission prompt", CWD: "/repos/alpha", PID: 502, StartedAt: ms(20)},
			{ID: "b3", SessionID: "s-done", Kind: "background", State: "done", CWD: "/repos/beta", StartedAt: ms(30)},
			{ID: "b4", SessionID: "s-failed", Kind: "background", State: "failed", CWD: "/repos/beta", StartedAt: ms(40)},
			{ID: "b5", SessionID: "s-stopped", Kind: "background", State: "stopped", CWD: "/repos/loose-place", StartedAt: ms(50)}, // loose
			// supervisor interactive attached in a tmux pane (dedupe case)
			{SessionID: "s-interactive", Kind: "interactive", Status: "busy", CWD: "/repos/alpha", PID: 601, StartedAt: ms(5), Name: "alpha-1a"},
		},
		Panes: []collect.Pane{
			// pane hosting the supervisor interactive session (dedupe: shell 600 -> claude 601)
			{SessionName: "alpha", WindowIndex: 1, PaneIndex: 1, PaneID: "%1", PanePID: 600, CurrentCommand: "claude", CurrentPath: "/repos/alpha", SessionAttached: true},
			// pane with a claude unknown to the supervisor
			{SessionName: "beta", WindowIndex: 2, PaneIndex: 1, PaneID: "%2", PanePID: 700, CurrentCommand: "zsh", CurrentPath: "/repos/beta", WindowName: "hack"},
			// pane with no claude at all
			{SessionName: "misc", WindowIndex: 1, PaneIndex: 1, PaneID: "%3", PanePID: 800, CurrentCommand: "vim", CurrentPath: "/tmp"},
		},
		Procs: []collect.Proc{
			{PID: 600, PPID: 1, Command: "bash"},
			{PID: 601, PPID: 600, Command: "claude"},
			{PID: 700, PPID: 1, Command: "zsh"},
			{PID: 701, PPID: 700, Command: "claude"}, // tmux-only session
			{PID: 800, PPID: 1, Command: "vim"},
		},
	}
}

func TestCorrelate_Scenario(t *testing.T) {
	snap := Correlate(fixtureInputs())

	// 7 sessions total: 6 supervisor + 1 tmux-interactive
	if len(snap.Sessions) != 7 {
		t.Fatalf("got %d sessions, want 7: %+v", len(snap.Sessions), keys(snap))
	}

	byID := map[string]*Session{}
	for _, s := range snap.Sessions {
		byID[s.SessionID] = s
	}

	// worktree binding: longest prefix wins
	if s := byID["s-working"]; s.Repo != "alpha" || s.Worktree != "/repos/alpha-worktrees/feat-kms" {
		t.Errorf("working session bound wrong: repo=%q wt=%q", s.Repo, s.Worktree)
	}
	if s := byID["s-blocked"]; s.Repo != "alpha" || s.Worktree != "/repos/alpha" {
		t.Errorf("blocked session bound wrong: %+v", s)
	}

	// loose session
	if s := byID["s-stopped"]; s.Repo != LooseRepoName {
		t.Errorf("stopped session should be loose, got repo=%q", s.Repo)
	}

	// dedupe: interactive session got the pane, no duplicate tmux session for %1
	if s := byID["s-interactive"]; s.TmuxTarget != "alpha:1.1" || s.TmuxPaneID != "%1" {
		t.Errorf("dedupe failed: %+v", s)
	}
	tmuxCount := 0
	for _, s := range snap.Sessions {
		if s.Kind == KindTmuxInteractive {
			tmuxCount++
			if s.TmuxPaneID != "%2" {
				t.Errorf("tmux-interactive in wrong pane: %+v", s)
			}
			if s.Repo != "beta" {
				t.Errorf("tmux-interactive should bind to beta: %+v", s)
			}
			if s.State != StateUnknown {
				t.Errorf("tmux-interactive state must be unknown: %q", s.State)
			}
		}
	}
	if tmuxCount != 1 {
		t.Errorf("got %d tmux-interactive sessions, want 1", tmuxCount)
	}

	// repo grouping: alpha, beta, (loose)
	names := []string{}
	for _, r := range snap.Repos {
		names = append(names, r.Name)
	}
	if len(snap.Repos) != 3 || snap.Repos[2].Name != LooseRepoName {
		t.Errorf("repos = %v, want [alpha beta (loose)]", names)
	}

	// ordering: needs-input first
	if !snap.Sessions[0].NeedsInput() {
		t.Errorf("first session should need input, got %+v", snap.Sessions[0])
	}

	// age computation
	if s := byID["s-working"]; s.AgeSeconds != 600 {
		t.Errorf("age = %d, want 600", s.AgeSeconds)
	}
}

func TestCorrelate_InteractiveStateMapping(t *testing.T) {
	in := fixtureInputs()
	snap := Correlate(in)
	byID := map[string]*Session{}
	for _, s := range snap.Sessions {
		byID[s.SessionID] = s
	}
	if s := byID["s-interactive"]; s.State != StateWorking {
		t.Errorf("busy interactive should map to working, got %q", s.State)
	}
}

func TestPathHasPrefix(t *testing.T) {
	tests := []struct {
		path, base string
		want       bool
	}{
		{"/a/b/c", "/a/b", true},
		{"/a/b", "/a/b", true},
		{"/a/bc", "/a/b", false},
		{"/a", "/a/b", false},
	}
	for _, tt := range tests {
		if got := pathHasPrefix(tt.path, tt.base); got != tt.want {
			t.Errorf("pathHasPrefix(%q,%q) = %v want %v", tt.path, tt.base, got, tt.want)
		}
	}
}

func keys(s *Snapshot) []string {
	var out []string
	for _, sess := range s.Sessions {
		out = append(out, sess.Key)
	}
	return out
}
