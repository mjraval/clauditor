package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Tonal surface system (TUI-CRAFT §2). The backdrop is never painted — the
// terminal's own background shows through the preview column and the window
// margins. Structure comes from three derived tones plus a hairline, each a
// mix() lerp over the theme anchors, so the list rail reads as a lifted surface
// against the terminal without any border chrome.
//
// adapted from agent-manager (Apache-2.0) internal/ui/{surface,theme}.go
// (mix lerp, derived tone ladder, paint primitive); re-implemented in Go.

// Theme anchors. Bg is the notional backdrop the tones lift off of; Surface is
// the lifted end of the ladder; Text anchors the hairline. Chosen to read as a
// quiet dark surface on a dark terminal (OSC-11 backdrop sync is v2b).
const (
	hexBg      = "#12151a"
	hexSurface = "#39414c"
	hexText    = "#c8d0d8"
)

// Derived tones (hex), computed once at package load.
var (
	tonePanel = mix(hexBg, hexSurface, 0.55) // list rail fill
	toneBlock = mix(hexBg, hexSurface, 0.35) // sheets / toasts
	toneRule  = mix(hexBg, hexText, 0.22)    // hairline seam / rules
)

// mix blends two hex colors; ratio 0 returns a, 1 returns b. Derived tones use
// it so the theme only declares its anchors.
func mix(a, b string, ratio float64) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	ar, ag, ab := hexRGB(a)
	br, bg, bb := hexRGB(b)
	blend := func(x, y int) int { return int(float64(x)*(1-ratio) + float64(y)*ratio + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", blend(ar, br), blend(ag, bg), blend(ab, bb))
}

func hexRGB(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return int(v>>16) & 0xff, int(v>>8) & 0xff, int(v) & 0xff
}

// bgSeq is the raw "set background" SGR for a hex color, routed through the live
// color profile so a 256-color terminal gets an indexed sequence. Returns ""
// on a no-color profile so paint degrades to an unpainted pad.
func bgSeq(hex string) string {
	seq := lipgloss.ColorProfile().Color(hex).Sequence(true)
	if seq == "" {
		return ""
	}
	return "\x1b[" + seq + "m"
}

// paint pads a possibly-styled line to an exact display width AND fills every
// cell with bg, re-asserting the bg after every inner SGR reset so per-segment
// lipgloss renders don't drop the fill partway across the row (TUI-CRAFT §2).
func paint(s string, width, _ int, bg string) string {
	if width <= 0 {
		return s
	}
	fill := bgSeq(bg)
	if fill == "" {
		return plainPad(s, width)
	}
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+fill)
	if w := ansi.StringWidth(s); w > width {
		s = ansi.Truncate(s, width, "…")
	} else if w < width {
		s += strings.Repeat(" ", width-w)
	}
	return fill + s + "\x1b[0m"
}

// plainPad pads a line to an exact display width without filling it, leaving the
// terminal's own background showing through (the preview column and captured
// output are drawn this way).
func plainPad(s string, width int) string {
	if width <= 0 {
		return s
	}
	if w := ansi.StringWidth(s); w > width {
		return ansi.Truncate(s, width, "…")
	} else if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// clampDisplay is the post-join width safety net (TUI-CRAFT §2): every assembled
// frame row is truncated to at most width display cells so nothing ever
// overflows into a neighboring column or wraps the frame.
func clampDisplay(s string, width int) string {
	if width <= 0 {
		return s
	}
	if ansi.StringWidth(s) > width {
		// "…" tail: a hard cut mid-word gives no cue content is missing
		// (QA finding at 60×15: header rendered "[in-pro").
		return ansi.Truncate(s, width, "…")
	}
	return s
}

// hrule is a full-width hairline in the rule tone.
func hrule(width int) string {
	if width < 1 {
		return ""
	}
	return styleRule.Render(strings.Repeat("─", width))
}

// --- pre-allocated styles (initStyles) --------------------------------------
//
// All lipgloss styles used per row/per frame are allocated once here, never
// with NewStyle() inside the render loop (TUI-CRAFT §2 style discipline). The
// state-color styles keep the frozen SPEC semantics (TUI-DESIGN §5).

var (
	// state / semantic roles (never repurposed)
	styleNeeds   lipgloss.Style
	styleWorking lipgloss.Style
	styleFailed  lipgloss.Style
	styleDim     lipgloss.Style
	styleAccent  lipgloss.Style
	styleErr     lipgloss.Style

	// chrome
	styleRepo    lipgloss.Style // repo group header (bold text)
	styleBucket  lipgloss.Style // bucket header (accent bold)
	styleTitle   lipgloss.Style // panel title (accent bold)
	styleRule    lipgloss.Style // hairline / seam
	stylePreview lipgloss.Style // dim preview / transcript body

	// footer
	styleFooterBar lipgloss.Style

	// selected inverted bar: dark text on the accent surface, bold
	styleSelText lipgloss.Style
)

func initStyles() {
	styleNeeds = lipgloss.NewStyle().Foreground(colNeeds).Bold(true)
	styleWorking = lipgloss.NewStyle().Foreground(colWorking)
	styleFailed = lipgloss.NewStyle().Foreground(colFailed).Underline(true)
	styleDim = lipgloss.NewStyle().Foreground(colDim)
	styleAccent = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	// Underlined: red is never hue-only (§5 colorblind rule — QA found the
	// stale chip, the app's core trust signal, was the one color-only red).
	styleErr = lipgloss.NewStyle().Foreground(colFailed).Bold(true).Underline(true)

	styleRepo = lipgloss.NewStyle().Foreground(lipgloss.Color(hexText)).Bold(true)
	styleBucket = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleTitle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleRule = lipgloss.NewStyle().Foreground(lipgloss.Color(toneRule))
	stylePreview = lipgloss.NewStyle().Foreground(colDim)

	styleFooterBar = lipgloss.NewStyle().Foreground(colDim)

	styleSelText = lipgloss.NewStyle().Foreground(lipgloss.Color(hexBg)).Bold(true)
}

func init() { initStyles() }
