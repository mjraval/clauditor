package tui

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mjraval/clauditor/internal/model"
	"github.com/mjraval/clauditor/internal/transcript"
)

// Preview cadence (TUI-CRAFT §1): fast while the selected session is actually
// working, calm otherwise; a short settle debounce after cursor movement so a
// held `j` discards stale captures instead of queueing tmux forks.
const (
	previewFast   = 300 * time.Millisecond
	previewCalm   = 1200 * time.Millisecond
	previewSettle = 50 * time.Millisecond
)

// previewResult is a fetched preview: which source produced it, the raw content
// (pane captures keep their ANSI; transcripts are already clean lines), and the
// caption source label.
type previewResult struct {
	kind   previewKind
	raw    string // pane: raw capture (ANSI kept). transcript: clean lines joined by \n
	source string // caption fragment: "pane dev:1.2" | "transcript" | ""
}

// fetchPreviewContent reads a session's preview from the correct local source,
// independent of the snapshot Source (both a tmux pane and the transcript file
// live on the same box as the cockpit): a live pane wins with a RAW
// ANSI-preserving `capture-pane -p -e`; a pane-less session with a sessionId
// falls back to its transcript tail rendered as ❯/●/⚒ lines. NO `claude logs`
// stripping — that is what mashed words together in v1 (TUI-CRAFT §1).
func fetchPreviewContent(ctx context.Context, sess *model.Session, lines int) (previewResult, error) {
	switch previewSourceKind(sess) {
	case previewPane:
		raw, err := capturePaneRaw(ctx, sess.TmuxPaneID, lines)
		return previewResult{kind: previewPane, raw: raw, source: "pane " + sess.TmuxTarget}, err
	case previewTranscript:
		out := transcript.Render(sess.SessionID, transcript.DefaultTailBytes)
		if lines > 0 && len(out) > lines {
			out = out[len(out)-lines:]
		}
		return previewResult{kind: previewTranscript, raw: strings.Join(out, "\n"), source: "transcript"}, nil
	default:
		return previewResult{kind: previewNone}, nil
	}
}

// capturePaneRaw runs `tmux capture-pane -p -e` for the last `lines` rows,
// keeping the ANSI so the preview shows the pane exactly as the human sees it.
// Argv array only, context-bound (exec discipline, CLAUDE.md).
func capturePaneRaw(ctx context.Context, paneID string, lines int) (string, error) {
	if lines < 1 {
		lines = 200
	}
	out, err := exec.CommandContext(ctx, "tmux", //nolint:gosec // fixed argv; paneID from the supervisor/tmux collector
		"capture-pane", "-p", "-e", "-t", paneID, "-S", "-"+strconv.Itoa(lines)).Output()
	return string(out), err
}
