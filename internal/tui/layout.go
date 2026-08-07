package tui

import (
	"strings"

	"github.com/rishi/clauditor/internal/model"
)

// wideThreshold is the terminal width at or above which the cockpit shows the
// session list and the live-preview pane side by side. Below it the list is
// full-width and `tab` toggles a full-screen preview of the selection.
const wideThreshold = 110

// wideLayout reports whether the horizontal split (list + preview) is used.
func wideLayout(width int) bool { return width >= wideThreshold }

// previewKind is which live source the preview pane reads for a session.
type previewKind int

const (
	previewNone previewKind = iota
	previewPane             // tmux capture-pane -p -t <paneID>
	previewLogs             // claude logs <id> (ANSI-stripped)
)

// previewSourceKind picks the preview source for a session: a live tmux pane
// wins (it shows the actual terminal a human sees), otherwise a background id
// falls back to `claude logs`. Sessions with neither have no preview. This is
// the local source's selection contract (the daemon /logs endpoint has its own
// fixed ID-first order); kept pure so it is unit-testable without a TTY.
func previewSourceKind(sess *model.Session) previewKind {
	switch {
	case sess == nil:
		return previewNone
	case sess.TmuxPaneID != "":
		return previewPane
	case sess.ID != "":
		return previewLogs
	default:
		return previewNone
	}
}

// replyEnabled reports whether the inline `r` reply action is offered for the
// selection: only a session that is actually waiting on a human AND has a
// background id we can attach to (docs/REPLY.md). Everything else is handled by
// attaching (enter).
func replyEnabled(sess *model.Session) bool {
	return sess != nil && sess.NeedsInput() && sess.ID != ""
}

// respawnEnabled reports whether the `R` respawn action applies: only a
// stopped or failed session that still has a background id.
func respawnEnabled(sess *model.Session) bool {
	return sess != nil && sess.ID != "" &&
		(sess.State == model.StateStopped || sess.State == model.StateFailed)
}

// tmuxSessionName extracts the tmux session name from a "session:window.pane"
// target (everything before the first ':'), for `tmux attach -t <session>`.
func tmuxSessionName(sess *model.Session) string {
	if sess == nil {
		return ""
	}
	if i := strings.IndexByte(sess.TmuxTarget, ':'); i > 0 {
		return sess.TmuxTarget[:i]
	}
	return sess.TmuxTarget
}

// lastNLines keeps the trailing n lines of s (trailing empty line trimmed
// first), so a full-screen ANSI replay fits the preview pane's height.
func lastNLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
