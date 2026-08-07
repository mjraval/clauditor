package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/rishi/clauditor/internal/model"
)

func TestPadTrunc(t *testing.T) {
	if got := padTrunc("abc", 6); got != "abc   " {
		t.Errorf("pad: %q", got)
	}
	if got := padTrunc("abcdefgh", 5); len([]rune(got)) != 5 {
		t.Errorf("truncate length = %d, want 5: %q", len([]rune(got)), got)
	}
	if got := padTrunc("abcdefgh", 5); !strings.HasSuffix(got, "…") {
		t.Errorf("truncated string should end in ellipsis: %q", got)
	}
	if got := padTrunc("abc", 0); got != "abc" {
		t.Errorf("width<=0 should pass through unchanged: %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("short string unchanged: %q", got)
	}
	if got := truncate("hello world", 5); got != "hell…" {
		t.Errorf("truncate(11,5) = %q, want %q", got, "hell…")
	}
}

func TestRowLine_SessionContainsExpectedFields(t *testing.T) {
	s := &model.Session{
		Name: "kms-rotation", State: model.StateWorking, ID: "abc123",
		TmuxTarget: "clauditor:2", WaitingFor: "", StartedAt: time.Now().Add(-90 * time.Second),
		AgeSeconds: 90,
	}
	row := Row{Kind: RowSession, Session: s}
	line := rowLine(row, 0, false) // width<=0: no padding, easier substring checks
	for _, want := range []string{"kms-rotation", "working", "clauditor:2", "abc123", "1m"} {
		if !strings.Contains(line, want) {
			t.Errorf("rowLine missing %q in %q", want, line)
		}
	}
}

func TestRowLine_SelectedHasMarker(t *testing.T) {
	s := &model.Session{Name: "x", State: model.StateIdle}
	row := Row{Kind: RowSession, Session: s}
	sel := rowLine(row, 0, true)
	unsel := rowLine(row, 0, false)
	if !strings.HasPrefix(sel, "> ") {
		t.Errorf("selected row should start with cursor marker: %q", sel)
	}
	if strings.HasPrefix(unsel, "> ") {
		t.Errorf("unselected row should not have cursor marker: %q", unsel)
	}
}

func TestRowLine_HeaderRows(t *testing.T) {
	if got := rowLine(Row{Kind: RowBucket, Text: "NEEDS INPUT (2)"}, 0, false); got != "NEEDS INPUT (2)" {
		t.Errorf("bucket row = %q", got)
	}
	if got := rowLine(Row{Kind: RowRepo, Text: "alpha"}, 0, false); !strings.Contains(got, "alpha") {
		t.Errorf("repo row missing name: %q", got)
	}
	wt := rowLine(Row{Kind: RowWorktree, Branch: "feat/x", Dirty: true, Managed: true}, 0, false)
	for _, want := range []string{"feat/x", "*dirty", "[claude-managed]"} {
		if !strings.Contains(wt, want) {
			t.Errorf("worktree row missing %q: %q", want, wt)
		}
	}
}

func TestRowLine_WidthIsRespected(t *testing.T) {
	s := &model.Session{Name: strings.Repeat("x", 200), State: model.StateWorking}
	row := Row{Kind: RowSession, Session: s}
	line := rowLine(row, 40, false)
	if got := len([]rune(line)); got != 40 {
		t.Errorf("rowLine width = %d, want 40: %q", got, line)
	}
}

func TestGlyph(t *testing.T) {
	cases := []struct {
		s    *model.Session
		want string
	}{
		{&model.Session{State: model.StateBlocked}, "◐"},
		{&model.Session{WaitingFor: "x"}, "◐"},
		{&model.Session{State: model.StateWorking}, "●"},
		{&model.Session{State: model.StateDone}, "✔"},
		{&model.Session{State: model.StateFailed}, "✕"},
		{&model.Session{State: model.StateStopped}, "⏹"},
		{&model.Session{State: model.StateIdle}, "○"},
	}
	for _, c := range cases {
		if got := glyph(c.s); got != c.want {
			t.Errorf("glyph(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestHeaderText_Counts(t *testing.T) {
	snap := fixtureSnapshot()
	got := HeaderText(snap, "daemon", time.Now().Add(-5*time.Second), time.Now(), FilterAll, "", "")
	for _, want := range []string{"1 need input", "1 working", "4 total", "[daemon]", "5s ago"} {
		if !strings.Contains(got, want) {
			t.Errorf("HeaderText missing %q in %q", want, got)
		}
	}
}

func TestHeaderText_ShowsFilterAndQuery(t *testing.T) {
	got := HeaderText(nil, "in-process", time.Time{}, time.Now(), FilterWorking, "kms", "")
	if !strings.Contains(got, "filter:working") {
		t.Errorf("HeaderText missing filter label: %q", got)
	}
	if !strings.Contains(got, "/kms") {
		t.Errorf("HeaderText missing query: %q", got)
	}
	if !strings.Contains(got, "never") {
		t.Errorf("zero lastFetch should render as never: %q", got)
	}
}

func TestHeaderText_SpinnerReplacesWorkingGlyph(t *testing.T) {
	snap := fixtureSnapshot()
	frame := spinnerFrames[0]
	got := HeaderText(snap, "daemon", time.Now(), time.Now(), FilterAll, "", frame)
	if !strings.Contains(got, frame) {
		t.Errorf("HeaderText should show the spinner frame %q when passed: %q", frame, got)
	}
	// With no spinner frame the static ● is shown instead.
	plain := HeaderText(snap, "daemon", time.Now(), time.Now(), FilterAll, "", "")
	if !strings.Contains(plain, "●") {
		t.Errorf("HeaderText should show ● when no spinner frame: %q", plain)
	}
}

func TestFooterText_ListModeHasCoreKeybindings(t *testing.T) {
	got := FooterText(footerList)
	for _, want := range []string{"enter", "r ", "o ", "d ", "x ", "R ", "l ", "/", "s ", "tab", "q "} {
		if !strings.Contains(got, want) {
			t.Errorf("list footer missing %q in %q", want, got)
		}
	}
}

func TestFooterText_ContextVariants(t *testing.T) {
	if !strings.Contains(FooterText(footerInput), "submit") {
		t.Errorf("input footer should mention submit: %q", FooterText(footerInput))
	}
	if !strings.Contains(FooterText(footerPreview), "tab") {
		t.Errorf("preview footer should mention tab: %q", FooterText(footerPreview))
	}
	if !strings.Contains(FooterText(footerLogs), "scroll") {
		t.Errorf("logs footer should mention scroll: %q", FooterText(footerLogs))
	}
}
