package main

import (
	"strings"
	"testing"
	"time"

	"github.com/mjraval/clauditor/internal/collect"
	"github.com/mjraval/clauditor/internal/model"
)

// The M2 acceptance scenario (SPEC §7.1): 2 repos, 3 worktrees, all five
// supervisor states, one tmux-interactive, one loose, one dedupe case —
// rendered as the grouped table.
func m2Snapshot() *model.Snapshot {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	ms := func(minAgo int) int64 { return now.Add(-time.Duration(minAgo) * time.Minute).UnixMilli() }
	return model.Correlate(model.Inputs{
		Now: now,
		Repos: []collect.RepoInfo{
			{Name: "alpha", Path: "/repos/alpha", Worktrees: []collect.WorktreeInfo{
				{Path: "/repos/alpha", Branch: "main", Dirty: "false", ManagedBy: "user"},
				{Path: "/repos/alpha-worktrees/feat-kms", Branch: "feat/kms", Dirty: "true", ManagedBy: "user"},
			}},
			{Name: "beta", Path: "/repos/beta", Worktrees: []collect.WorktreeInfo{
				{Path: "/repos/beta", Branch: "main", Dirty: "unknown", ManagedBy: "user"},
			}},
		},
		Agents: []collect.AgentEntry{
			{ID: "b1", SessionID: "s-working", Kind: "background", State: "working", Status: "busy", CWD: "/repos/alpha-worktrees/feat-kms", PID: 501, StartedAt: ms(10), Name: "kms rotation"},
			{ID: "b2", SessionID: "s-blocked", Kind: "background", State: "blocked", Status: "waiting", WaitingFor: "permission prompt", CWD: "/repos/alpha", PID: 502, StartedAt: ms(20), Name: "blocked one"},
			{ID: "b3", SessionID: "s-done", Kind: "background", State: "done", CWD: "/repos/beta", StartedAt: ms(30), Name: "done one"},
			{ID: "b4", SessionID: "s-failed", Kind: "background", State: "failed", CWD: "/repos/beta", StartedAt: ms(40), Name: "failed one"},
			{ID: "b5", SessionID: "s-stopped", Kind: "background", State: "stopped", CWD: "/somewhere/else", StartedAt: ms(50), Name: "stopped one"},
			{SessionID: "s-interactive", Kind: "interactive", Status: "busy", CWD: "/repos/alpha", PID: 601, StartedAt: ms(5), Name: "alpha-1a"},
		},
		Panes: []collect.Pane{
			{SessionName: "alpha", WindowIndex: 1, PaneIndex: 1, PaneID: "%1", PanePID: 600, CurrentCommand: "claude", CurrentPath: "/repos/alpha", SessionAttached: true},
			{SessionName: "beta", WindowIndex: 2, PaneIndex: 1, PaneID: "%2", PanePID: 700, CurrentCommand: "zsh", CurrentPath: "/repos/beta", WindowName: "hack"},
		},
		Procs: []collect.Proc{
			{PID: 600, PPID: 1, Command: "bash"},
			{PID: 601, PPID: 600, Command: "claude"},
			{PID: 700, PPID: 1, Command: "zsh"},
			{PID: 701, PPID: 700, Command: "claude"},
		},
	})
}

func TestRenderStatus_M2Scenario(t *testing.T) {
	var b strings.Builder
	renderStatus(&b, m2Snapshot(), false)
	out := b.String()

	for _, want := range []string{
		"alpha", "beta", "(loose)", // all groups
		"feat/kms", "●dirty", // worktree row with dirty dot
		"blocked: permission prompt", // waitingFor inline
		"◐",                          // needs-input glyph
		"●",                          // working glyph
		"✔",                          // done glyph
		"✕",                          // failed glyph
		"⏹",                          // stopped glyph
		"○",                          // unknown glyph (tmux-interactive)
		"⧉ alpha:1.1",                // dedupe case carries its tmux target
		"⧉ beta:2.1",                 // tmux-interactive pane target
		"1 need input",               // header counter: the blocked session
		"2 working",                  // bg working + busy interactive
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderStatus_Empty(t *testing.T) {
	var b strings.Builder
	renderStatus(&b, &model.Snapshot{}, false)
	if !strings.Contains(b.String(), "no repos configured") {
		t.Errorf("empty snapshot message wrong: %s", b.String())
	}
}

// TestRenderStatus_CostColumn: the --cost flag (or [usage].track_cost)
// shows a fleet-total header segment and a per-session tok/cost suffix,
// but only for sessions the poller actually priced (CostKnown) — an
// unpriced session must not show a misleading "$0.00", and the whole
// feature must be invisible when showCost is false even if the snapshot
// carries cost data (e.g. a prior enrichment run).
func TestRenderStatus_CostColumn(t *testing.T) {
	snap := m2Snapshot()
	priced := snap.Sessions[0]
	priced.CostKnown = true
	priced.Tokens = 1_200_000
	priced.CostMicroUSD = 3_400_000 // $3.40
	unpriced := snap.Sessions[1]
	unpriced.CostKnown = false
	unpriced.Tokens = 500

	var off strings.Builder
	renderStatus(&off, snap, false)
	if strings.Contains(off.String(), "$3.40") {
		t.Errorf("cost column should not render when showCost=false:\n%s", off.String())
	}

	var on strings.Builder
	renderStatus(&on, snap, true)
	out := on.String()
	if !strings.Contains(out, "1.2M tok · $3.40") {
		t.Errorf("priced session missing tok/cost suffix:\n%s", out)
	}
	if !strings.Contains(out, "$3.40 working") {
		t.Errorf("fleet-total header segment missing:\n%s", out)
	}
	// The unpriced session's name should appear without any $ figure right
	// after it (best-effort substring check: its own line has no "$").
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, displayName(unpriced)) && strings.Contains(line, "$") {
			t.Errorf("unpriced session line must not show a cost figure: %q", line)
		}
	}
}
