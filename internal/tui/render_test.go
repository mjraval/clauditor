package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/mjraval/clauditor/internal/model"
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
	for _, want := range []string{"kms-rotation", "⧉ clauditor:2", "1m"} {
		if !strings.Contains(line, want) {
			t.Errorf("rowLine missing %q in %q", want, line)
		}
	}
	// §5 anatomy cleanup: the state word and the short id are gone from rows.
	for _, gone := range []string{"working", "abc123"} {
		if strings.Contains(line, gone) {
			t.Errorf("rowLine should no longer render %q: %q", gone, line)
		}
	}
}

func TestSessionBody_BadgePriorityAndDegradation(t *testing.T) {
	tmux := &model.Session{Kind: model.KindSupervisorBG, State: model.StateWorking,
		Name: "payments-recon", TmuxTarget: "dev:1.2", AgeSeconds: 7800}
	bare := &model.Session{Kind: model.KindSupervisorInteractive, State: model.StateWorking,
		Name: "payments-recon", AgeSeconds: 7800}

	// Wide: ⧉ carries its target; age is right-aligned (last visible token).
	wide := sessionBody(tmux, 60)
	if !strings.Contains(wide, "⧉ dev:1.2") {
		t.Errorf("wide tmux row should show target: %q", wide)
	}
	if !strings.HasSuffix(strings.TrimRight(wide, " "), "2h 10m") {
		t.Errorf("age should be right-aligned: %q", wide)
	}

	// Medium: the ⧉ target text drops first, the glyph stays.
	med := sessionBody(tmux, 30)
	if strings.Contains(med, "dev:1.2") {
		t.Errorf("medium tmux row should drop the target text: %q", med)
	}
	if !strings.Contains(med, "⧉") {
		t.Errorf("medium tmux row should keep the ⧉ glyph: %q", med)
	}

	// Very narrow: the whole ⧉ badge drops (durability is informational).
	tight := sessionBody(tmux, 14)
	if strings.Contains(tight, "⧉") {
		t.Errorf("very narrow tmux row should drop ⧉ entirely: %q", tight)
	}

	// The ⌁bare risk badge NEVER drops across the usable width range (the list
	// clamps to ≥38 cols, i.e. inner ≥32), and wins as the only badge.
	for _, inner := range []int{60, 30, 14} {
		body := sessionBody(bare, inner)
		if !strings.Contains(body, "⌁bare") {
			t.Errorf("bare row at inner=%d must keep ⌁bare: %q", inner, body)
		}
		if strings.Contains(body, "⧉") {
			t.Errorf("bare row must not show ⧉ (⌁bare wins): %q", body)
		}
	}
}

func TestRowLine_SelectedHasMarker(t *testing.T) {
	s := &model.Session{Name: "x", State: model.StateIdle}
	row := Row{Kind: RowSession, Session: s}
	sel := rowLine(row, 0, true)
	unsel := rowLine(row, 0, false)
	if !strings.HasPrefix(sel, "▶ ") {
		t.Errorf("selected row should start with cursor marker: %q", sel)
	}
	if strings.HasPrefix(unsel, "▶ ") {
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
	for _, want := range []string{"1 need input", "1 working", "4 total", "[daemon]", "5s"} {
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

func TestFooterForSelection_ContextualAndBounded(t *testing.T) {
	cases := []struct {
		name string
		sess *model.Session
		want string
	}{
		{"nothing selected", nil, "d dispatch · / filter · ? help · q quit"},
		{"blocked with id", &model.Session{State: model.StateBlocked, ID: "abc"},
			"r reply · enter attach · o tmux · l logs · / filter · ? help"},
		{"bare interactive", &model.Session{Kind: model.KindSupervisorInteractive, State: model.StateWorking},
			"enter attach · D make durable · l logs · d dispatch · / filter · ? help"},
		{"working durable", &model.Session{Kind: model.KindSupervisorBG, State: model.StateWorking, ID: "x", TmuxTarget: "dev:1"},
			"enter attach · o tmux · x stop · d dispatch · / filter · ? help"},
		{"stopped", &model.Session{Kind: model.KindSupervisorBG, State: model.StateStopped, ID: "x"},
			"R respawn · l logs · d dispatch · / filter · ? help"},
		{"idle", &model.Session{Kind: model.KindSupervisorBG, State: model.StateIdle, ID: "x", TmuxTarget: "dev:1"},
			"enter attach · o tmux · x stop · l logs · / filter · ? help"},
	}
	for _, c := range cases {
		got := footerForSelection(c.sess)
		if got != c.want {
			t.Errorf("%s: footer = %q, want %q", c.name, got, c.want)
		}
		// Bar 3: max 6 hints, always ending "? help".
		hints := strings.Split(got, " · ")
		if len(hints) > 6 {
			t.Errorf("%s: %d hints, want ≤6: %q", c.name, len(hints), got)
		}
		last := hints[len(hints)-1]
		if last != "? help" && last != "q quit" {
			t.Errorf("%s: footer must end in ? help (or q quit when empty): %q", c.name, got)
		}
	}
}

func TestFooterText_ModalVariants(t *testing.T) {
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

func TestFreshnessChip_StalePast15s(t *testing.T) {
	now := time.Now()
	if got := stripANSI(freshnessChip(now.Add(-3*time.Second), now, 0)); got != "3s" {
		t.Errorf("fresh chip = %q, want bare 3s", got)
	}
	got := stripANSI(freshnessChip(now.Add(-22*time.Second), now, 0))
	if got != "stale 22s — retrying" {
		t.Errorf("stale chip = %q, want %q", got, "stale 22s — retrying")
	}
	if got := stripANSI(freshnessChip(time.Time{}, now, 0)); got != "never" {
		t.Errorf("zero chip = %q, want never", got)
	}
	// Regression (QA fidelity): in-process mode fetches never fail, so a dead
	// supervisor must surface via the collector age, not the snapshot age.
	got = stripANSI(freshnessChip(now.Add(-1*time.Second), now, 21))
	if got != "stale 21s — retrying" {
		t.Errorf("supervisor-age staleness = %q, want %q", got, "stale 21s — retrying")
	}
}

func TestCollectorFailSegments_OnlyOnFailure(t *testing.T) {
	// Healthy collectors: no segments (calm is a feature).
	if segs := collectorFailSegments(map[string]int64{"claude": 2, "tmux": 4, "git": 11}); len(segs) != 0 {
		t.Errorf("healthy collectors should emit no segments, got %v", segs)
	}
	// A behind tmux collector shows a red ✗ segment; never (-1) stays silent.
	segs := collectorFailSegments(map[string]int64{"claude": 2, "tmux": 40, "git": -1})
	if len(segs) != 1 || !strings.Contains(stripANSI(segs[0]), "tmux scan ✗ 40s") {
		t.Errorf("expected one 'tmux scan ✗ 40s' segment, got %v", segs)
	}
}

func TestSourcesLine(t *testing.T) {
	got := sourcesLine(map[string]int64{"claude": 2, "tmux": 4, "git": 11})
	want := "sources: supervisor ✓ 2s · tmux ✓ 4s · git ✓ 11s"
	if got != want {
		t.Errorf("sourcesLine = %q, want %q", got, want)
	}
	if got := sourcesLine(map[string]int64{"claude": -1, "tmux": 4, "git": 11}); !strings.Contains(got, "supervisor never") {
		t.Errorf("never collector should read 'never': %q", got)
	}
}

func TestHumanAge_TwoComponent(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{3 * time.Second, "0s"},                     // quantized down to a 5s step
		{47 * time.Second, "45s"},                   // 5s steps
		{45 * time.Minute, "45m"},                   // single component under an hour
		{3*time.Hour + 20*time.Minute, "3h 20m"},    // two components
		{2 * time.Hour, "2h"},                       // secondary dropped when zero
		{2*24*time.Hour + 5*time.Hour, "2d 5h"},     // days + hours
		{9 * 24 * time.Hour, "1w 2d"},               // weeks + days
	}
	for _, c := range cases {
		if got := humanAge(c.d); got != c.want {
			t.Errorf("humanAge(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestHumanDur_Days(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{2*time.Hour + 10*time.Minute, "2h10m"},
		{47 * time.Hour, "47h00m"},
		{48 * time.Hour, "2d"},
		{72 * time.Hour, "3d"},
	}
	for _, c := range cases {
		if got := humanDur(c.d); got != c.want {
			t.Errorf("humanDur(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}
