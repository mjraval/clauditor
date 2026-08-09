package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestStripDangerSeqs(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\x1b[2Kb", "ab"},           // erase-line
		{"a\x1b[Jb", "ab"},            // erase-screen
		{"a\x1b[3Sb", "ab"},           // scroll-up
		{"a\x1b[1;5rb", "ab"},         // set scroll region
		{"a\x1bDb", "ab"},             // 7-bit index
		{"a\x1bMb", "ab"},             // 7-bit reverse index
		{"keep\x1b[31mred\x1b[0m", "keep\x1b[31mred\x1b[0m"}, // SGR color survives
	}
	for _, c := range cases {
		if got := stripDangerSeqs(c.in); got != c.want {
			t.Errorf("stripDangerSeqs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDropControlChars(t *testing.T) {
	got := dropControlChars("a\rb\tc\x07d\x1b[0m")
	if got != "ab\tcd\x1b[0m" { // \r and \x07 dropped; TAB and ESC kept
		t.Errorf("dropControlChars = %q", got)
	}
}

func TestSanitizeCaptureLine_ClampAndReset(t *testing.T) {
	// An ESC-bearing line gets a trailing reset so its color can't bleed.
	line := sanitizeCaptureLine("\x1b[31mhello world", 0)
	if !strings.HasSuffix(line, "\x1b[0m") {
		t.Errorf("ESC-bearing line must end in a reset: %q", line)
	}
	// Width clamp is escape-aware: display width never exceeds the budget.
	clamped := sanitizeCaptureLine("\x1b[31m"+strings.Repeat("x", 40), 10)
	if w := ansi.StringWidth(clamped); w > 10 {
		t.Errorf("clamped width = %d, want ≤10: %q", w, clamped)
	}
}

func TestCollapseBlankRuns(t *testing.T) {
	in := []string{"a", "", "", "", "b", "", ""}
	got := collapseBlankRuns(in)
	want := []string{"a", "", "", "b"} // >2 blanks collapsed to 2, trailing trimmed
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("collapseBlankRuns = %v, want %v", got, want)
	}
}

func TestSanitizeCapture_FullPipeline(t *testing.T) {
	raw := "\x1b[2K\x1b[31mline one\r\n\n\n\n\x1b[32mline two\n\n"
	got := SanitizeCapture(raw, 40)
	// The 3-blank run collapses to 2 and the trailing blanks are trimmed:
	// line one · "" · "" · line two.
	want := []string{"line one", "", "", "line two"}
	if len(got) != len(want) {
		t.Fatalf("want %d lines after collapse/trim, got %d: %#v", len(want), len(got), got)
	}
	if got[len(got)-1] == "" {
		t.Errorf("trailing blank not trimmed: %#v", got)
	}
	if strings.Contains(strings.Join(got, ""), "\x1b[2K") {
		t.Errorf("dangerous erase-line survived: %#v", got)
	}
	for _, l := range got {
		if strings.ContainsRune(l, 0x1b) && !strings.HasSuffix(l, "\x1b[0m") {
			t.Errorf("colored line not reset-terminated: %q", l)
		}
	}
}

// Regression (QA): complete OSC sequences (BEL- or ST-terminated) are stripped
// whole — dropping only the BEL would create the unterminated-OSC hazard.
func TestSanitizeCapture_OSCStrippedWhole(t *testing.T) {
	in := "before \x1b]0;evil-title\x07after"
	got := sanitizeCaptureLine(in, 80)
	if strings.Contains(got, "\x1b]") || strings.Contains(got, "evil-title") {
		t.Errorf("OSC not stripped whole: %q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("surrounding text lost: %q", got)
	}
	// ST-terminated hyperlink: escapes go, visible text stays.
	link := "\x1b]8;;http://x\x1b\\click\x1b]8;;\x1b\\"
	got = sanitizeCaptureLine(link, 80)
	if strings.Contains(got, "\x1b]") {
		t.Errorf("OSC-8 not stripped: %q", got)
	}
	if !strings.Contains(got, "click") {
		t.Errorf("hyperlink text lost: %q", got)
	}
	// Unterminated OSC at line end: loses to EOL (safe direction).
	got = sanitizeCaptureLine("txt \x1b]0;unterminated", 80)
	if strings.Contains(got, "\x1b]") {
		t.Errorf("unterminated OSC survived: %q", got)
	}
}

// Regression (QA): cursor movement/addressing and DSR are stripped even though
// capture-pane never emits them — the sanitizer must stay safe for any caller.
func TestSanitizeCapture_CursorAndDSRStripped(t *testing.T) {
	in := "a\x1b[2Ab\x1b[10;20Hc\x1b[6nd\x1b[31mred"
	got := sanitizeCaptureLine(in, 80)
	for _, bad := range []string{"\x1b[2A", "\x1b[10;20H", "\x1b[6n"} {
		if strings.Contains(got, bad) {
			t.Errorf("dangerous seq %q survived: %q", bad, got)
		}
	}
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("SGR color wrongly stripped: %q", got)
	}
	if !strings.Contains(got, "a") || !strings.Contains(got, "red") {
		t.Errorf("text lost: %q", got)
	}
}
