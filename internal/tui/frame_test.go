package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mjraval/clauditor/internal/model"
)

// stubSource is a no-op Source so renderFrame (which calls m.source.Label()) can
// run without a live daemon/collector.
type stubSource struct{}

func (stubSource) Fetch(context.Context) (*model.Snapshot, error) { return nil, nil }
func (stubSource) Label() string                                  { return "in-process" }

// paneModel builds a rendered-ready model with a single working supervisor
// session that lives in a tmux pane, selected, at the given size.
func paneModel(w, h int) Model {
	s := &model.Session{
		Key: "sup-abc", Kind: model.KindSupervisorBG, State: model.StateWorking,
		Name: "payments-recon", ID: "abc", SessionID: "uuid-abc",
		TmuxPaneID: "%1", TmuxTarget: "dev:1.2", AgeSeconds: 90,
	}
	repo := &model.Repo{Name: "stables", Path: "/r", Worktrees: []*model.Worktree{
		{Path: "/r", Branch: "main", Sessions: []*model.Session{s}},
	}}
	m := Model{cursor: -1, width: w, height: h, source: stubSource{}}
	m.snap = &model.Snapshot{Repos: []*model.Repo{repo}, Sessions: []*model.Session{s}}
	m.rebuildRows()
	m.cursor = FirstSelectable(m.rows)
	return m
}

// TestFrame_NoOverflow_PathologicalPreview is the §2/§C width-discipline
// regression: a pathological long unbroken preview line must never overflow —
// every rendered row is at most the frame width in display cells, and the frame
// is exactly height rows.
func TestFrame_NoOverflow_PathologicalPreview(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 20}, {110, 30}, {140, 40}} {
		m := paneModel(size.w, size.h)
		m.previewKey = m.selectedSession().Key
		m.previewKind = previewPane
		m.previewSource = "pane dev:1.2"
		m.previewAt = time.Now()
		// One unbroken 5000-char line, plus a colored line whose SGR is never
		// closed (would bleed without the sanitation kit's trailing reset).
		m.previewRaw = strings.Repeat("x", 5000) + "\n\x1b[31m" + strings.Repeat("y", 5000)

		frame := m.renderFrame()
		rows := strings.Split(frame, "\n")
		if len(rows) != size.h {
			t.Errorf("%dx%d: frame has %d rows, want %d", size.w, size.h, len(rows), size.h)
		}
		for i, r := range rows {
			if got := ansi.StringWidth(r); got > size.w {
				t.Errorf("%dx%d: row %d width = %d, want ≤ %d: %q", size.w, size.h, i, got, size.w, r)
			}
		}
	}
}

// TestFrame_ToastNoLayoutShift asserts the toast overlay changes no row count
// and no row width (it is spliced in place).
func TestFrame_ToastNoLayoutShift(t *testing.T) {
	m := paneModel(140, 40)
	m.previewKey = m.selectedSession().Key
	m.previewKind = previewTranscript
	m.previewRaw = "❯ hello\n● hi there"
	before := strings.Split(m.renderFrame(), "\n")

	m.statusMsg, m.statusErr, m.statusAt = "dispatched in /r · xyz", false, time.Now()
	after := strings.Split(m.renderFrame(), "\n")

	if len(before) != len(after) {
		t.Fatalf("toast changed row count: %d → %d", len(before), len(after))
	}
	for i := range after {
		if ansi.StringWidth(after[i]) > 140 {
			t.Errorf("row %d overflows with toast: %d", i, ansi.StringWidth(after[i]))
		}
	}
	if strings.Join(before, "\n") == strings.Join(after, "\n") {
		t.Errorf("toast should have changed the frame content")
	}
}

func TestNarrowListFooter_TabPreviewWithinCap(t *testing.T) {
	// A 5-hint selection footer (stopped) gains "tab preview" (→6, still ≤6).
	stopped := &model.Session{Kind: model.KindSupervisorBG, State: model.StateStopped, ID: "x"}
	got := narrowListFooter(stopped)
	if !strings.Contains(got, "tab preview") {
		t.Errorf("narrow footer should add tab preview when there is room: %q", got)
	}
	if n := len(strings.Split(got, " · ")); n > 6 {
		t.Errorf("narrow footer exceeds 6 hints: %d (%q)", n, got)
	}
	// A 6-hint selection footer (blocked) has no room — left unchanged.
	blocked := &model.Session{State: model.StateBlocked, ID: "x"}
	if got := narrowListFooter(blocked); strings.Contains(got, "tab preview") {
		t.Errorf("full footer must not exceed the cap by adding tab: %q", got)
	}
}

func TestToastLines_FitsWidthAndReturnsNilWhenTooNarrow(t *testing.T) {
	box := toastLines("dispatched in /repo · abc123", false, 140)
	if len(box) == 0 {
		t.Fatal("expected a toast box at 140 cols")
	}
	for _, l := range box {
		if ansi.StringWidth(l) > toastMaxWidth {
			t.Errorf("toast row wider than max: %d", ansi.StringWidth(l))
		}
	}
	if toastLines("x", false, 10) != nil {
		t.Errorf("toast should be nil on a too-narrow frame")
	}
}
