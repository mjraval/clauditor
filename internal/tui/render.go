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
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

// runeLen is the display width in runes (all glyphs here are single-cell).
func runeLen(s string) int { return len([]rune(s)) }

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

// ageText is the right-aligned age cell, "" when the session has no start time.
func ageText(s *model.Session) string {
	if s.AgeSeconds <= 0 && s.StartedAt.IsZero() {
		return ""
	}
	return humanDur(time.Duration(s.AgeSeconds) * time.Second)
}

// sessionBody builds the plain (unstyled) text for one session row's content
// area (glyph + name + waitingFor + badge + right-aligned age), filling exactly
// `inner` runes. Anatomy and degradation follow TUI-DESIGN.md §5: the state
// word and short id are gone (glyph+color already say state; id lives in the
// preview caption); at most one badge is shown, ⌁bare (risk) winning over ⧉
// (durability). As width shrinks: the ⧉ target text drops, then the ⧉ glyph,
// then waitingFor truncates 16→8, then the name truncates 24→14. The glyph, the
// ⌁bare risk badge, and the age never drop.
func sessionBody(s *model.Session, inner int) string {
	g := glyph(s)
	age := ageText(s)
	name := displayName(s)
	if inner < 8 {
		return truncate(g+" "+name, inner)
	}

	// Right edge reserves the age plus a one-space gap; the left region gets
	// whatever is left over.
	leftBudget := inner - runeLen(g) - 1 // glyph + trailing space
	if age != "" {
		leftBudget -= runeLen(age) + 1 // age cell + min gap
	}
	if leftBudget < 4 {
		leftBudget = 4
	}

	bare := sessionBare(s)
	var badgeFull, badgeMin, finalBadge string
	switch {
	case bare:
		badgeFull, badgeMin, finalBadge = "⌁bare", "⌁bare", "⌁bare" // never drops
	case s.TmuxTarget != "":
		badgeFull, badgeMin, finalBadge = "⧉ "+s.TmuxTarget, "⧉", ""
	}

	// Degradation candidates, richest → poorest (the §5 order).
	type cand struct {
		nameCap, wfCap int
		badge          string
	}
	cands := []cand{
		{24, 16, badgeFull},
		{24, 16, badgeMin},
	}
	if !bare {
		cands = append(cands, cand{24, 16, ""}) // drop the ⧉ glyph too
	}
	cands = append(cands,
		cand{24, 8, finalBadge},
		cand{14, 8, finalBadge},
	)

	build := func(c cand) string {
		left := truncate(name, c.nameCap)
		if s.WaitingFor != "" && c.wfCap > 0 {
			left += " ⚑ " + truncate(s.WaitingFor, c.wfCap)
		}
		if c.badge != "" {
			left += " " + c.badge
		}
		return left
	}
	left := build(cands[len(cands)-1])
	for _, c := range cands {
		if b := build(c); runeLen(b) <= leftBudget {
			left = b
			break
		}
	}
	if runeLen(left) > leftBudget {
		// Last resort: sacrifice the name, never the ⌁bare risk badge.
		suffix := ""
		if bare {
			suffix = " ⌁bare"
		}
		if room := leftBudget - runeLen(suffix); room < 1 {
			left = strings.TrimSpace(suffix) // no room for a name: keep just the badge
		} else {
			left = truncate(name, room) + suffix
		}
		if runeLen(left) > leftBudget {
			left = truncate(left, leftBudget)
		}
	}

	out := g + " " + left
	if age == "" {
		return padTrunc(out, inner)
	}
	gap := inner - runeLen(out) - runeLen(age)
	if gap < 1 {
		gap = 1
	}
	return padTrunc(out+strings.Repeat(" ", gap)+age, inner)
}

// rowLine renders a Row's plain text (no ANSI), including the cursor marker
// and indentation, then pads/truncates it to width. Kept separate from
// styling so it's testable without any lipgloss/terminal dependency.
func rowLine(row Row, width int, selected bool) string {
	marker := "  "
	if selected {
		marker = "> "
	}
	if row.Kind == RowSession {
		const prefix = 6 // marker(2) + indent(4)
		inner := width - prefix
		if width <= 0 {
			inner = 96 // no terminal size yet: show everything (test/inspection path)
		}
		if inner < 4 {
			inner = 4
		}
		line := marker + "    " + sessionBody(row.Session, inner)
		if width <= 0 {
			return line
		}
		return padTrunc(line, width)
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
		freshnessChip(lastFetch, now),
	}
	line := strings.Join(segs, dot)
	if filter != FilterAll {
		line += dot + styleAccent.Render("filter:"+filter.String())
	}
	if query != "" {
		line += dot + styleAccent.Render("/"+query)
	}
	// Collector-failure segments — appended in red only when a collector is
	// behind; healthy collectors stay silent (calm is a feature, §3).
	if snap != nil {
		for _, seg := range collectorFailSegments(snap.CollectorAges) {
			line += dot + seg
		}
	}
	return line
}

// freshnessChip is the snapshot-age chip: bare dim "3s" while fresh, red
// "stale 22s — retrying" once the last good snapshot is older than 15s (§5).
func freshnessChip(lastFetch, now time.Time) string {
	if lastFetch.IsZero() {
		return styleDim.Render("never")
	}
	secs := int(now.Sub(lastFetch).Seconds())
	if secs < 0 {
		secs = 0
	}
	if secs > 15 {
		return styleErr.Render(fmt.Sprintf("stale %ds — retrying", secs))
	}
	return styleDim.Render(fmt.Sprintf("%ds", secs))
}

// collectorFailSegments returns red header segments for collectors that have
// fallen behind their poll cadence (roughly 3× the interval). Never-succeeded
// collectors (-1) stay silent to keep startup calm.
func collectorFailSegments(ages map[string]int64) []string {
	if ages == nil {
		return nil
	}
	order := []struct {
		key, name string
		thresh    int64
	}{
		{"claude", "supervisor", 15},
		{"tmux", "tmux", 30},
		{"git", "git", 60},
	}
	var segs []string
	for _, c := range order {
		age, ok := ages[c.key]
		if !ok || age < 0 || age <= c.thresh {
			continue
		}
		segs = append(segs, styleErr.Render(fmt.Sprintf("%s scan ✗ %s", c.name, humanDur(time.Duration(age)*time.Second))))
	}
	return segs
}

// sourcesLine is the `?` overlay footer that doubles as the completeness
// report: "supervisor ✓ 2s · tmux ✓ 4s · git ✓ 11s" (§3, §4). A collector that
// has never reported reads "never".
func sourcesLine(ages map[string]int64) string {
	order := []struct{ key, name string }{
		{"claude", "supervisor"},
		{"tmux", "tmux"},
		{"git", "git"},
	}
	parts := make([]string, 0, len(order))
	for _, c := range order {
		age, ok := ages[c.key]
		switch {
		case !ok || age < 0:
			parts = append(parts, fmt.Sprintf("%s never", c.name))
		default:
			parts = append(parts, fmt.Sprintf("%s ✓ %s", c.name, humanDur(time.Duration(age)*time.Second)))
		}
	}
	return "sources: " + strings.Join(parts, " · ")
}

// footerKind selects which context-sensitive key-hint bar to show.
type footerKind int

const (
	footerList footerKind = iota
	footerPreview
	footerInput
	footerLogs
)

// FooterText is the context-sensitive keybinding hint bar for the modal/pager
// footers (input, logs, narrow-preview). List-mode footers are selection-driven
// — see footerForSelection.
func FooterText(kind footerKind) string {
	var s string
	switch kind {
	case footerLogs:
		s = "j/k scroll · pgup/pgdn page · q back"
	case footerInput:
		s = "enter submit · esc cancel"
	case footerPreview:
		s = "tab list · enter attach · l logs · q quit · ? help"
	default: // footerList (no selection context available)
		s = footerForSelection(nil)
	}
	return styleFooterBar.Render(s)
}

// footerForSelection returns the max-6-hint list footer (always ending
// `? help`) for the current selection's state — a verb appears only when it
// would work right now, first hint = most likely intent (§4 footer table).
// Returned unstyled so callers can accent the lead hint (the first-blocked
// flash); styleFooterList styles the whole bar.
func footerForSelection(sess *model.Session) string {
	switch {
	case sess == nil:
		return "d dispatch · / filter · ? help · q quit"
	case replyEnabled(sess): // blocked, has id
		return "r reply · enter attach · o tmux · l logs · / filter · ? help"
	case sessionBare(sess): // bare interactive (fragile)
		return "enter attach · D make durable · l logs · d dispatch · / filter · ? help"
	case sess.State == model.StateWorking: // working, durable
		return "enter attach · o tmux · x stop · d dispatch · / filter · ? help"
	case sess.State == model.StateStopped || sess.State == model.StateFailed:
		return "R respawn · l logs · d dispatch · / filter · ? help"
	default: // idle / unknown / done
		return "enter attach · o tmux · x stop · l logs · / filter · ? help"
	}
}

// styleFooterList renders a list footer, optionally accenting a leading
// "r reply" hint for the one-cycle first-blocked flash (§4).
func styleFooterList(text string, flashReply bool) string {
	if flashReply && strings.HasPrefix(text, "r reply · ") {
		return styleAccent.Render("r reply") + styleFooterBar.Render(strings.TrimPrefix(text, "r reply"))
	}
	return styleFooterBar.Render(text)
}

// ErrorText styles a transient error/status line.
func ErrorText(msg string) string {
	return styleErr.Render(msg)
}
