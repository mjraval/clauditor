package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mjraval/clauditor/internal/model"
)

// Toasts (TUI-CRAFT §4): action feedback is a small card spliced over the
// already-rendered frame's top-right, so a notice costs the body zero rows —
// nothing ever shifts. adapted from agent-manager (Apache-2.0)
// internal/ui/toast.go; re-implemented in Go.

const (
	toastMaxWidth = 52
	toastMinWidth = 14
	toastMargin   = 2
	toastChromeX  = 4 // "│ " + " │"
)

// toastBox returns the painted toast card for the current action feedback, or
// nil when there is nothing to show / the message has aged out.
func (m Model) toastBox(frameWidth int) []string {
	if m.statusMsg == "" || time.Since(m.statusAt) > toastTTL {
		return nil
	}
	return toastLines(m.statusMsg, m.statusErr, frameWidth)
}

// toastLines renders a message as a bordered card (block tone), red-bordered on
// error, wrapped to at most a few lines. Returns nil when the frame is too
// narrow to host even a minimal card.
func toastLines(text string, isErr bool, frameWidth int) []string {
	width := ansi.StringWidth(text) + toastChromeX
	for _, limit := range []int{toastMaxWidth, frameWidth - toastMargin - 2} {
		if width > limit {
			width = limit
		}
	}
	if width < toastMinWidth {
		return nil
	}
	inner := width - toastChromeX
	border := styleRule
	if isErr {
		border = styleErr
	}
	edge := border.Render("│")
	wrapped := strings.Split(ansi.Wordwrap(text, inner, ""), "\n")
	if len(wrapped) > 3 { // keep it a glance, not a paragraph
		wrapped = wrapped[:3]
	}
	rows := []string{paint(border.Render("╭"+strings.Repeat("─", width-2)+"╮"), width, 0, toneBlock)}
	for _, ln := range wrapped {
		rows = append(rows, paint(edge+" "+padTrunc(ln, inner)+" "+edge, width, 0, toneBlock))
	}
	return append(rows, paint(border.Render("╰"+strings.Repeat("─", width-2)+"╯"), width, 0, toneBlock))
}

// spliceTopRight overpaints a pre-painted box over the frame's rows, anchored at
// row `top` and inset from the right edge — rows past the frame's bottom are
// dropped rather than extending it, so the layout never moves.
func spliceTopRight(frame string, box []string, top, frameWidth int) string {
	if len(box) == 0 {
		return frame
	}
	left := frameWidth - maxBoxWidth(box) - toastMargin
	if left < 0 {
		left = 0
	}
	lines := strings.Split(frame, "\n")
	for i, patch := range box {
		row := top + i
		if row < 0 || row >= len(lines) {
			continue
		}
		lines[row] = spliceAtColumn(lines[row], patch, left)
	}
	return strings.Join(lines, "\n")
}

// spliceAtColumn overpaints a run of cells inside a styled row at display column
// left, keeping the escape state on both sides of the patch intact.
func spliceAtColumn(row, patch string, left int) string {
	head := ansi.Truncate(row, left, "")
	if pad := left - ansi.StringWidth(head); pad > 0 {
		head += strings.Repeat(" ", pad)
	}
	tail := ansi.TruncateLeft(row, left+ansi.StringWidth(patch), "")
	return head + patch + tail
}

// maxBoxWidth is the widest display width across the box's rows.
func maxBoxWidth(box []string) int {
	w := 0
	for _, l := range box {
		if x := ansi.StringWidth(l); x > w {
			w = x
		}
	}
	return w
}

// narrowListFooter is the list footer on a narrow terminal: the selection's
// contextual hints, with a live `tab preview` affordance inserted before
// `? help` when there is room (≤6 hints total, so the cap holds) — a footer
// label that carries live state where it is cheap (TUI-CRAFT §4).
func narrowListFooter(sess *model.Session) string {
	base := footerForSelection(sess)
	hints := strings.Split(base, " · ")
	if len(hints) >= 6 { // no room without exceeding the cap
		return base
	}
	last := hints[len(hints)-1] // "? help" (or "q quit" when empty)
	hints = append(hints[:len(hints)-1], "tab preview", last)
	return strings.Join(hints, " · ")
}
