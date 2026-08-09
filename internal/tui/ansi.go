package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ANSI passthrough sanitation kit — the four steps that make it SAFE to keep a
// captured terminal's colors in the preview pane instead of stripping them
// (TUI-CRAFT §1). A `tmux capture-pane -p -e` grab is an already-rendered 2D
// grid, but it still carries escape sequences that, embedded verbatim in a
// bubbletea View(), would scroll or clear the HOST terminal, or bleed an
// unterminated color into the neighboring column.
//
// adapted from agent-manager (Apache-2.0) internal/ui/view.go previewLine
// (dangerous-seq strip + C0 drop + width clamp + trailing reset) and
// internal/ui/... blank-run handling. Re-implemented in Go for clauditor.

// dangerSeqRe matches the display-damaging sequences: erase-line/screen (K/J),
// scroll (S/T), insert/delete-line (L/M), set-scroll-region (r), cursor
// movement/addressing (A/B/C/D/E/F/G/H/f/d — capture-pane output never
// contains these because tmux's VT parser resolves motion into the grid, but
// this sanitizer must stay safe for ANY future caller, QA finding), the
// device-status query (CSI n, would make the host terminal WRITE a reply),
// and the 7-bit index / reverse-index / next-line controls (ESC D / ESC M /
// ESC E). These would move, clear, or interrogate the outer cockpit terminal.
var dangerSeqRe = regexp.MustCompile(`\x1b\[[0-9;?]*[KJLMSTrABCDEFGHfdn]|\x1b[DEM]`)

// oscSeqRe matches complete OSC sequences (title-set, hyperlinks, clipboard —
// terminated by BEL or ST) plus DCS/PM/APC strings. Stripped whole BEFORE the
// C0 pass: dropping a BEL terminator on its own would convert an already-
// terminated (harmless) OSC into an unterminated one — exactly the failure
// this kit exists to prevent (QA finding). Unterminated OSC/DCS at
// end-of-line loses everything to the line end, which is the safe direction.
var oscSeqRe = regexp.MustCompile(`\x1b[\]PX^_][^\x07\x1b]*(\x07|\x1b\\|$)`)

// stripDangerSeqs removes the host-terminal-damaging escapes (step 1).
func stripDangerSeqs(s string) string {
	s = oscSeqRe.ReplaceAllString(s, "")
	return dangerSeqRe.ReplaceAllString(s, "")
}

// dropControlChars drops C0 control characters except ESC (0x1b, still needed
// for SGR color) and TAB (0x09) — killing \r, \b and friends that corrupt
// column layout (step 2). Operates per line, so newlines are already gone.
func dropControlChars(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != 0x1b && r != '\t' {
			return -1
		}
		return r
	}, s)
}

// sanitizeCaptureLine runs steps 1–3 on one captured line: strip dangerous
// sequences, drop C0 controls, width-clamp with an escape-aware truncate, and
// terminate any ESC-bearing line with a reset so an open SGR cannot bleed into
// the next column or the following frame (step 3).
func sanitizeCaptureLine(line string, width int) string {
	line = stripDangerSeqs(line)
	line = dropControlChars(line)
	if width > 0 && ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	if strings.ContainsRune(line, 0x1b) {
		line += "\x1b[0m"
	}
	return line
}

// collapseBlankRuns collapses runs of more than two blank lines to two and
// trims trailing blanks (step 4), so a short capture shows content rather than
// a column of cursor padding before the height truncation.
func collapseBlankRuns(lines []string) []string {
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, l := range lines {
		if strings.TrimSpace(stripDangerSeqs(l)) == "" {
			blanks++
			if blanks > 2 {
				continue
			}
			out = append(out, l)
			continue
		}
		blanks = 0
		out = append(out, l)
	}
	for len(out) > 0 && strings.TrimSpace(stripDangerSeqs(out[len(out)-1])) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// SanitizeCapture is the full kit over a raw `capture-pane -e` blob: split into
// lines, sanitize each to at most width cells, then collapse/trim blank runs.
// The result keeps the pane's colors, safe to splice into a View().
func SanitizeCapture(raw string, width int) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	for i, l := range lines {
		lines[i] = sanitizeCaptureLine(l, width)
	}
	return collapseBlankRuns(lines)
}
