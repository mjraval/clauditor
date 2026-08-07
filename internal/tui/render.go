package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rishi/clauditor/internal/model"
)

// Styles (SPEC §11 / dispatch prompt): needs-input yellow, working green,
// failed red+underline, idle/terminal dim. Colors are ANSI 16-color codes so
// they degrade sanely over a 200ms SSH link with a basic terminfo.
var (
	styleNeeds     = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	styleWorking   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleFailed    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Underline(true)
	styleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleRepo      = lipgloss.NewStyle().Bold(true)
	styleBucket    = lipgloss.NewStyle().Bold(true).Underline(true)
	styleHeaderBar = lipgloss.NewStyle().Bold(true)
	styleFooterBar = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleErr       = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
)

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

// HeaderText builds the one-line header: session counts, data-source
// indicator, and last-refresh age (SPEC §11 acceptance: show enough state
// to trust a possibly-stale view over a slow link).
func HeaderText(snap *model.Snapshot, sourceLabel string, lastFetch, now time.Time, filter StateFilter, query string) string {
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
	line := fmt.Sprintf("clauditor tui · %d need input · %d working · %d total · [%s] · refreshed %s",
		needs, working, total, sourceLabel, age)
	if filter != FilterAll {
		line += fmt.Sprintf(" · filter:%s", filter.Label())
	}
	if query != "" {
		line += fmt.Sprintf(" · /%s", query)
	}
	return styleHeaderBar.Render(line)
}

// FooterText is the keybinding hint bar.
func FooterText() string {
	return styleFooterBar.Render("j/k↑↓ move · / filter · s state · enter open-tmux · l logs · d dispatch · x stop · q quit")
}

// ErrorText styles a transient error/status line.
func ErrorText(msg string) string {
	return styleErr.Render(msg)
}
