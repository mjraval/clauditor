package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rishi/clauditor/internal/model"
)

// Cockpit palette — matches the WebUI (web/static): terminal-default bg,
// accent #d97a4a, needs-input #e8b44c, working #4caf7d, failed #e05d5d,
// dim #6d7b89. lipgloss down-samples these hexes to the nearest color on a
// low-color terminal, so they still degrade sanely over a slow SSH link.
var (
	colAccent  = lipgloss.Color("#d97a4a")
	colNeeds   = lipgloss.Color("#e8b44c")
	colWorking = lipgloss.Color("#4caf7d")
	colFailed  = lipgloss.Color("#e05d5d")
	colDim     = lipgloss.Color("#6d7b89")
)

var (
	styleNeeds     = lipgloss.NewStyle().Foreground(colNeeds).Bold(true)
	styleWorking   = lipgloss.NewStyle().Foreground(colWorking)
	styleFailed    = lipgloss.NewStyle().Foreground(colFailed).Underline(true)
	styleDim       = lipgloss.NewStyle().Foreground(colDim)
	styleAccent    = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleRepo      = lipgloss.NewStyle().Bold(true)
	styleBucket    = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleFooterBar = lipgloss.NewStyle().Foreground(colDim)
	styleErr       = lipgloss.NewStyle().Foreground(colFailed).Bold(true)
	styleCaption   = lipgloss.NewStyle().Foreground(colAccent)
	stylePreview   = lipgloss.NewStyle().Foreground(colDim)
	styleSep       = lipgloss.NewStyle().Foreground(colDim)
)

// spinnerFrames animate the working indicator in the header (braille).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// glyph is the state marker (same visual language as `clauditor status`,
// see cmd/clauditor/status.go's glyph()).
func glyph(s *model.Session) string {
	switch {
	case s.NeedsInput():
		return "◐"
	case s.State == model.StateWorking:
		return "●"
	case s.State == model.StateDone:
		return "✔"
	case s.State == model.StateFailed:
		return "✕"
	case s.State == model.StateStopped:
		return "⏹"
	default:
		return "○"
	}
}

func displayName(s *model.Session) string {
	if s.Name != "" {
		return s.Name
	}
	if s.Kind == model.KindTmuxInteractive {
		return "(interactive in tmux)"
	}
	return "(unnamed)"
}

func humanDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// truncate shortens s to at most n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// padTrunc forces s to exactly width runes: truncated with an ellipsis if
// longer, space-padded if shorter. width<=0 returns s unchanged (caller
// didn't know the terminal size yet).
func padTrunc(s string, width int) string {
	if width <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) > width {
		return truncate(s, width)
	}
	return s + strings.Repeat(" ", width-len(r))
}

// sessionLine builds the plain (unstyled) text for one session row.
func sessionLine(s *model.Session) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %-22s %-9s", glyph(s), truncate(displayName(s), 22), s.State)
	if s.WaitingFor != "" {
		fmt.Fprintf(&b, " ⚑ %s", truncate(s.WaitingFor, 16))
	}
	if s.TmuxTarget != "" {
		fmt.Fprintf(&b, " ⧉ %s", s.TmuxTarget)
	}
	if !s.StartedAt.IsZero() {
		fmt.Fprintf(&b, " · %s", humanDur(time.Duration(s.AgeSeconds)*time.Second))
	}
	if s.ID != "" {
		fmt.Fprintf(&b, " · %s", s.ID)
	}
	return b.String()
}

// rowLine renders a Row's plain text (no ANSI), including the cursor marker
// and indentation, then pads/truncates it to width. Kept separate from
// styling so it's testable without any lipgloss/terminal dependency.
func rowLine(row Row, width int, selected bool) string {
	marker := "  "
	if selected {
		marker = "> "
	}
	var s string
	switch row.Kind {
	case RowBucket:
		s = row.Text
	case RowRepo:
		s = "  " + row.Text
	case RowWorktree:
		extra := ""
		if row.Dirty {
			extra += " *dirty"
		}
		if row.Managed {
			extra += " [claude-managed]"
		}
		s = "    " + row.Branch + extra
	case RowSession:
		s = marker + "    " + sessionLine(row.Session)
	}
	return padTrunc(s, width)
}

// rowStyle picks the lipgloss style for a row. Selected session rows keep
// their state color but reverse fg/bg so the highlight survives on
// low-color terminals.
func rowStyle(row Row, selected bool) lipgloss.Style {
	var style lipgloss.Style
	switch row.Kind {
	case RowBucket:
		style = styleBucket
	case RowRepo:
		style = styleRepo
	case RowWorktree:
		style = styleDim
	case RowSession:
		style = sessionStyle(row.Session)
	}
	if selected {
		style = style.Reverse(true)
	}
	return style
}

// sessionStyle is the state → color mapping (SPEC-driven: needs-input
// yellow/bold, working green, failed red+underline, everything else dim).
func sessionStyle(s *model.Session) lipgloss.Style {
	switch {
	case s.NeedsInput():
		return styleNeeds
	case s.State == model.StateWorking:
		return styleWorking
	case s.State == model.StateFailed:
		return styleFailed
	default:
		return styleDim
	}
}

// RenderRow is the styled line for one Row at the given terminal width.
func RenderRow(row Row, width int, selected bool) string {
	return rowStyle(row, selected).Render(rowLine(row, width, selected))
}

// HeaderText builds the one-line cockpit header: app name, needs-input count
// (yellow ◐), working count (green ●, or the braille spinner frame when any
// session is working), total, data-source label, and last-refresh age. The
// spinner argument is the current animation frame ("" = show the static ●).
// Kept a pure function so the header content is unit-testable without a TTY.
func HeaderText(snap *model.Snapshot, sourceLabel string, lastFetch, now time.Time, filter StateFilter, query, spinner string) string {
	needs, working, total := 0, 0, 0
	if snap != nil {
		total = len(snap.Sessions)
		for _, s := range snap.Sessions {
			if s.NeedsInput() {
				needs++
			} else if s.State == model.StateWorking {
				working++
			}
		}
	}
	age := "never"
	if !lastFetch.IsZero() {
		age = humanDur(now.Sub(lastFetch)) + " ago"
	}
	workGlyph := "●"
	if spinner != "" {
		workGlyph = spinner
	}
	dot := styleDim.Render(" · ")
	segs := []string{
		styleAccent.Render("clauditor"),
		styleNeeds.Render(fmt.Sprintf("◐ %d need input", needs)),
		styleWorking.Render(fmt.Sprintf("%s %d working", workGlyph, working)),
		styleDim.Render(fmt.Sprintf("%d total", total)),
		styleDim.Render("["+sourceLabel+"]"),
		styleDim.Render("refreshed " + age),
	}
	line := strings.Join(segs, dot)
	if filter != FilterAll {
		line += dot + styleDim.Render("filter:"+filter.Label())
	}
	if query != "" {
		line += dot + styleDim.Render("/"+query)
	}
	return line
}

// footerKind selects which context-sensitive key-hint bar to show.
type footerKind int

const (
	footerList footerKind = iota
	footerPreview
	footerInput
	footerLogs
)

// FooterText is the context-sensitive keybinding hint bar.
func FooterText(kind footerKind) string {
	var s string
	switch kind {
	case footerLogs:
		s = "j/k ↑↓ scroll · pgup/pgdn page · q/esc back"
	case footerInput:
		s = "enter submit · esc cancel"
	case footerPreview:
		s = "tab list · enter attach · r reply · l logs · q quit"
	default: // footerList
		s = "↑↓ nav · enter attach · r reply · o tmux · d dispatch · x stop · R respawn · l logs · / filter · s state · tab preview · q quit"
	}
	return styleFooterBar.Render(s)
}

// ErrorText styles a transient error/status line.
func ErrorText(msg string) string {
	return styleErr.Render(msg)
}
