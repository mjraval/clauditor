package tui

import (
	"fmt"
	"strings"

	"github.com/rishi/clauditor/internal/model"
)

// durability classifies a session by whether it survives terminal/SSH loss and
// which `D` (make-durable) behavior applies. Mirrors the TUI-DESIGN.md §6 table
// exactly. Kept pure so the badge, footer, and `D` dispatch are unit-testable
// without a TTY.
type durability int

const (
	// durBare is a supervisor-interactive session in a bare terminal (no tmux
	// pane): the ONLY fragile kind. `D` opens the make-durable sheet.
	durBare durability = iota
	// durBackgroundLoose is a supervisor-bg session not visible in tmux:
	// daemon-owned, survives on its own.
	durBackgroundLoose
	// durBackgroundTmux is a supervisor-bg session also visible in a tmux pane.
	durBackgroundTmux
	// durInteractiveTmux is a supervisor-interactive session living in tmux.
	durInteractiveTmux
	// durTmuxInteractive is a claude process observed directly in a tmux pane.
	durTmuxInteractive
)

// durabilityOf classifies a session per §6. A nil session is treated as
// background-loose (nothing fragile to warn about).
func durabilityOf(s *model.Session) durability {
	if s == nil {
		return durBackgroundLoose
	}
	switch s.Kind {
	case model.KindTmuxInteractive:
		return durTmuxInteractive
	case model.KindSupervisorInteractive:
		if s.TmuxTarget == "" {
			return durBare
		}
		return durInteractiveTmux
	default: // KindSupervisorBG (and any unknown kind: treat as daemon-owned)
		if s.TmuxTarget == "" {
			return durBackgroundLoose
		}
		return durBackgroundTmux
	}
}

// sessionBare reports whether the session is fragile — a supervisor-interactive
// session in a bare terminal that dies with its terminal/SSH connection. This
// is the ⌁bare badge predicate and the sheet trigger.
func sessionBare(s *model.Session) bool { return durabilityOf(s) == durBare }

// durabilityAction is what pressing `D` does for a selection: either open the
// make-durable sheet (openSheet=true), or answer with a toast (msg) because the
// session is already durable.
func durabilityAction(s *model.Session) (msg string, openSheet bool) {
	switch durabilityOf(s) {
	case durBare:
		return "", true
	case durBackgroundLoose:
		return "already durable — background sessions survive terminal loss · o opens it in tmux", false
	case durBackgroundTmux:
		return fmt.Sprintf("already durable — daemon-owned and visible in tmux (%s)", s.TmuxTarget), false
	default: // durInteractiveTmux, durTmuxInteractive
		return fmt.Sprintf("already durable — lives in tmux (%s)", s.TmuxTarget), false
	}
}

// sheetInnerWidth is the make-durable sheet's content width between borders;
// sheetWidth is the full box width (borders included).
const (
	sheetInnerWidth = 65
	sheetWidth      = sheetInnerWidth + 4
)

// makeDurableSheet renders the centered make-durable sheet (§6, exact copy) for
// a bare session, as styled lines each exactly sheetWidth runes wide.
func makeDurableSheet(s *model.Session) []string {
	body := []string{
		"This session runs in a bare terminal. If that terminal or its",
		"SSH connection dies, the session dies with it.",
		"",
		" t   continue in tmux  (recommended)",
		"     Opens a tmux window running `claude --resume` on this",
		"     conversation. Durable from then on. The original terminal",
		"     keeps a live copy of the old session — exit it when you",
		"     get back to it, and don't type into both.",
		"",
		" b   background it from the inside",
		"     No external command can background a bare interactive",
		"     session. Attach now (this key does it) and press ← or",
		"     type /bg inside — it becomes a daemon-owned background",
		"     session and survives on its own.",
		"",
		" esc cancel",
	}
	const inner = sheetInnerWidth // content width between the "│ " and " │" borders
	title := "Make durable — " + displayName(s)

	lines := make([]string, 0, len(body)+2)
	// Top border: ┌ <title> ─…─┐  (total width = inner + 4)
	head := "┌ " + truncate(title, inner-1) + " "
	dash := inner + 4 - runeLen(head) - 1 // -1 for the closing ┐
	if dash < 1 {
		dash = 1
	}
	lines = append(lines, styleAccent.Render(head+strings.Repeat("─", dash)+"┐"))
	for _, ln := range body {
		txt := padTrunc(ln, inner)
		lines = append(lines, styleAccent.Render("│ ")+styleDim.Render(txt)+styleAccent.Render(" │"))
	}
	lines = append(lines, styleAccent.Render("└"+strings.Repeat("─", inner+2)+"┘"))
	return lines
}
