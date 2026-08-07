package tui

import (
	"testing"

	"github.com/rishi/clauditor/internal/model"
)

func sess(key, state string, waitingFor string) *model.Session {
	return &model.Session{Key: key, Name: key, State: state, WaitingFor: waitingFor}
}

func fixtureSnapshot() *model.Snapshot {
	needsSess := sess("needs1", model.StateBlocked, "")
	workingSess := sess("work1", model.StateWorking, "")
	idleSess := sess("idle1", model.StateIdle, "")
	doneSess := sess("done1", model.StateDone, "")

	repoAlpha := &model.Repo{Name: "alpha", Path: "/repos/alpha", Worktrees: []*model.Worktree{
		{Path: "/repos/alpha", Branch: "main", Sessions: []*model.Session{needsSess, workingSess}},
	}}
	repoBeta := &model.Repo{Name: "beta", Path: "/repos/beta", Worktrees: []*model.Worktree{
		{Path: "/repos/beta", Branch: "feat/x", Dirty: "true", Sessions: []*model.Session{idleSess, doneSess}},
	}}
	all := []*model.Session{needsSess, workingSess, idleSess, doneSess}
	for _, r := range []*model.Repo{repoAlpha, repoBeta} {
		for _, wt := range r.Worktrees {
			for _, s := range wt.Sessions {
				s.Repo = r.Name
				s.Worktree = wt.Path
			}
		}
	}
	return &model.Snapshot{Repos: []*model.Repo{repoAlpha, repoBeta}, Sessions: all}
}

func TestBucketOf(t *testing.T) {
	cases := []struct {
		s    *model.Session
		want string
	}{
		{&model.Session{State: model.StateBlocked}, "needs"},
		{&model.Session{State: model.StateWorking, WaitingFor: "approval"}, "needs"}, // WaitingFor wins regardless of State
		{&model.Session{State: model.StateWorking}, "working"},
		{&model.Session{State: model.StateIdle}, "idle"},
		{&model.Session{State: model.StateUnknown}, "idle"},
		{&model.Session{State: model.StateDone}, "terminal"},
		{&model.Session{State: model.StateFailed}, "terminal"},
		{&model.Session{State: model.StateStopped}, "terminal"},
	}
	for _, c := range cases {
		if got := bucketOf(c.s); got != c.want {
			t.Errorf("bucketOf(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestBuildRows_BucketOrderAndGrouping(t *testing.T) {
	rows := BuildRows(fixtureSnapshot(), "", FilterAll)

	var sessionKeys []string
	for _, r := range rows {
		if r.Session != nil {
			sessionKeys = append(sessionKeys, r.Session.Key)
		}
	}
	// Needs-input bucket must appear before working, before idle, before terminal.
	wantOrder := []string{"needs1", "work1", "idle1", "done1"}
	if len(sessionKeys) != len(wantOrder) {
		t.Fatalf("session keys = %v, want %v", sessionKeys, wantOrder)
	}
	for i, want := range wantOrder {
		if sessionKeys[i] != want {
			t.Errorf("position %d: session = %q, want %q (order %v)", i, sessionKeys[i], want, sessionKeys)
		}
	}
	// First row must be a bucket header, and it must be the needs-input one.
	if rows[0].Kind != RowBucket || rows[0].Bucket != "needs" {
		t.Errorf("first row = %+v, want needs bucket header", rows[0])
	}
}

func TestBuildRows_EmptyBucketOmitted(t *testing.T) {
	snap := fixtureSnapshot() // has no "terminal"-only session missing, all 4 buckets present
	rows := BuildRows(snap, "", FilterWorking)
	for _, r := range rows {
		if r.Kind == RowBucket && r.Bucket != "working" {
			t.Errorf("filtered-out bucket leaked into rows: %+v", r)
		}
	}
	// Only one session row (work1) should be selectable.
	var n int
	for _, r := range rows {
		if r.Selectable() {
			n++
		}
	}
	if n != 1 {
		t.Errorf("selectable rows = %d, want 1", n)
	}
}

func TestBuildRows_QueryFiltersSessionsAndDropsEmptyGroups(t *testing.T) {
	rows := BuildRows(fixtureSnapshot(), "work1", FilterAll)
	var repos []string
	for _, r := range rows {
		if r.Kind == RowRepo {
			repos = append(repos, r.Text)
		}
	}
	if len(repos) != 1 || repos[0] != "alpha" {
		t.Errorf("repos = %v, want only alpha (beta has no session matching the query)", repos)
	}
}

func TestBuildRows_WorktreeBranchLabel(t *testing.T) {
	rows := BuildRows(fixtureSnapshot(), "", FilterAll)
	var branches []string
	for _, r := range rows {
		if r.Kind == RowWorktree {
			branches = append(branches, r.Branch)
		}
	}
	found := map[string]bool{}
	for _, b := range branches {
		found[b] = true
	}
	if !found["main"] || !found["feat/x"] {
		t.Errorf("branches = %v, want main and feat/x present", branches)
	}
}

func TestBuildRows_NilSnapshot(t *testing.T) {
	if rows := BuildRows(nil, "", FilterAll); rows != nil {
		t.Errorf("nil snapshot should yield nil rows, got %v", rows)
	}
}

func TestNextPrevSelectable(t *testing.T) {
	rows := []Row{
		{Kind: RowBucket},
		{Kind: RowSession, Session: &model.Session{Key: "a"}},
		{Kind: RowWorktree},
		{Kind: RowSession, Session: &model.Session{Key: "b"}},
	}
	if idx := NextSelectable(rows, -1); idx != 1 {
		t.Errorf("NextSelectable(-1) = %d, want 1", idx)
	}
	if idx := NextSelectable(rows, 1); idx != 3 {
		t.Errorf("NextSelectable(1) = %d, want 3", idx)
	}
	if idx := NextSelectable(rows, 3); idx != 1 { // wraps
		t.Errorf("NextSelectable(3) wrap = %d, want 1", idx)
	}
	if idx := PrevSelectable(rows, 1); idx != 3 { // wraps backward
		t.Errorf("PrevSelectable(1) wrap = %d, want 3", idx)
	}
	if idx := PrevSelectable(rows, 3); idx != 1 {
		t.Errorf("PrevSelectable(3) = %d, want 1", idx)
	}
}

func TestNextSelectable_NoneSelectable(t *testing.T) {
	rows := []Row{{Kind: RowBucket}, {Kind: RowRepo}}
	if idx := NextSelectable(rows, 0); idx != -1 {
		t.Errorf("NextSelectable with no selectable rows = %d, want -1", idx)
	}
}

func TestClampCursor(t *testing.T) {
	rows := []Row{
		{Kind: RowBucket},
		{Kind: RowSession, Session: &model.Session{Key: "a"}},
		{Kind: RowWorktree},
	}
	if idx := ClampCursor(rows, 0); idx != 1 {
		t.Errorf("ClampCursor(0) = %d, want 1 (nearest selectable forward)", idx)
	}
	if idx := ClampCursor(rows, 2); idx != 1 {
		t.Errorf("ClampCursor(2) = %d, want 1 (nearest selectable backward)", idx)
	}
	if idx := ClampCursor(nil, 0); idx != -1 {
		t.Errorf("ClampCursor(nil) = %d, want -1", idx)
	}
	noneSelectable := []Row{{Kind: RowBucket}, {Kind: RowRepo}}
	if idx := ClampCursor(noneSelectable, 0); idx != -1 {
		t.Errorf("ClampCursor with no selectable rows = %d, want -1", idx)
	}
}

func TestVisibleWindow(t *testing.T) {
	if start, end := VisibleWindow(10, 3, 20); start != 0 || end != 10 {
		t.Errorf("short list: got (%d,%d), want (0,10)", start, end)
	}
	start, end := VisibleWindow(100, 50, 10)
	if end-start != 10 {
		t.Errorf("window size = %d, want 10", end-start)
	}
	if start > 50 || end <= 50 {
		t.Errorf("cursor 50 not within window [%d,%d)", start, end)
	}
	// Cursor near the end must not push start past total-height.
	start, end = VisibleWindow(100, 99, 10)
	if end != 100 || start != 90 {
		t.Errorf("end-clamped window = (%d,%d), want (90,100)", start, end)
	}
	// Cursor near the start must not go negative.
	start, end = VisibleWindow(100, 0, 10)
	if start != 0 || end != 10 {
		t.Errorf("start-clamped window = (%d,%d), want (0,10)", start, end)
	}
}
