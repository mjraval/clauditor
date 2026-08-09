package tui

import (
	"strings"

	"github.com/mjraval/clauditor/internal/version"
)

// helpRows are the two-column GLANCE/ACT body of the `?` overlay (§4). Section
// headers (GLANCE, ACT ON SELECTION, MOVE) carry no leading space; hint rows do.
var helpRows = []struct{ left, right string }{
	{"GLANCE", "ACT ON SELECTION"},
	{" /       filter as you type", "enter  attach (the obvious thing)"},
	{" 1–4     only needs / working /", "       come back: ← at the empty"},
	{"         idle / done", "       prompt · tmux jumps: prefix+L"},
	{" esc     clear filter / overlay", "r      reply to a blocked session"},
	{" tab     fullscreen preview", "o      open in tmux, don't switch"},
	{"         (narrow terminals)", "D      make durable (bare sessions)"},
	{"", "d      dispatch background task here"},
	{"MOVE", "x      stop… asks first"},
	{" j/k ↑↓  select session", "R      respawn stopped/failed"},
	{" g / G   first / last", "l      logs pager"},
	{" ^d/^u   half page", "COMING (v1.1): n new session ·"},
	{" q       quit — instant, no prompt", "h resume conversation · : commands"},
}

// helpLines renders the `?` overlay as exactly `height` full-width lines: a
// titled header, a two-column key crib, and the sources/version footer that
// doubles as the completeness report (§4).
func helpLines(width, height int, ages map[string]int64, sourceLabel string) []string {
	const leftCol = 34
	out := make([]string, 0, height)

	out = append(out, styleAccent.Render(spread("  clauditor — keys", "esc or ? closes", width)))
	out = append(out, styleDim.Render(rule(width)))
	for i, r := range helpRows {
		line := "  " + padTrunc(r.left, leftCol) + " " + r.right
		line = padTrunc(line, width)
		// Section headers (GLANCE/ACT row 0, MOVE mid-list) render in accent.
		if i == 0 || strings.HasPrefix(r.left, "MOVE") {
			out = append(out, styleAccent.Render(line))
		} else {
			out = append(out, styleDim.Render(line))
		}
	}
	out = append(out, styleDim.Render(rule(width)))
	out = append(out, styleDim.Render(spread("  "+sourcesLine(ages), "v"+version.Version+" · "+sourceLabel, width)))

	for len(out) < height {
		out = append(out, styleDim.Render(strings.Repeat(" ", max0(width))))
	}
	return out[:max0(height)]
}

// spread lays left flush-left and right flush-right within width (min one gap),
// padded/truncated to exactly width.
func spread(left, right string, width int) string {
	gap := width - runeLen(left) - runeLen(right)
	if gap < 1 {
		return padTrunc(left+" "+right, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// rule is the overlay's horizontal divider, inset two columns.
func rule(width int) string {
	n := width - 2
	if n < 0 {
		n = 0
	}
	return "  " + strings.Repeat("─", n)
}

// overlayCenter composites already-styled box lines (each exactly boxW wide)
// centered over a plain-text background, dimming everything the box does not
// cover — the §6 "centered box, list dimmed behind" look. bg lines must be
// plain (no ANSI) so rune slicing stays correct.
func overlayCenter(bg, box []string, boxW, width, height int) []string {
	top := (height - len(box)) / 2
	if top < 0 {
		top = 0
	}
	left := (width - boxW) / 2
	if left < 0 {
		left = 0
	}
	out := make([]string, height)
	for i := 0; i < height; i++ {
		r := []rune("")
		if i < len(bg) {
			r = []rune(bg[i])
		}
		for len(r) < width {
			r = append(r, ' ')
		}
		r = r[:width]
		bi := i - top
		if bi < 0 || bi >= len(box) {
			out[i] = styleDim.Render(string(r))
			continue
		}
		rightStart := left + boxW
		if rightStart > width {
			rightStart = width
		}
		out[i] = styleDim.Render(string(r[:left])) + box[bi] + styleDim.Render(string(r[rightStart:]))
	}
	return out
}
