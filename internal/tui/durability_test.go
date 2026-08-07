package tui

import (
	"strings"
	"testing"

	"github.com/rishi/clauditor/internal/model"
)

func TestDurabilityOf_And_Bare(t *testing.T) {
	cases := []struct {
		name     string
		sess     *model.Session
		wantDur  durability
		wantBare bool
	}{
		{"nil", nil, durBackgroundLoose, false},
		{"bg loose", &model.Session{Kind: model.KindSupervisorBG}, durBackgroundLoose, false},
		{"bg in tmux", &model.Session{Kind: model.KindSupervisorBG, TmuxTarget: "dev:1.2"}, durBackgroundTmux, false},
		{"interactive in tmux", &model.Session{Kind: model.KindSupervisorInteractive, TmuxTarget: "dev:1.2"}, durInteractiveTmux, false},
		{"interactive bare", &model.Session{Kind: model.KindSupervisorInteractive}, durBare, true},
		{"tmux interactive", &model.Session{Kind: model.KindTmuxInteractive, TmuxTarget: "dev:1.2"}, durTmuxInteractive, false},
	}
	for _, c := range cases {
		if got := durabilityOf(c.sess); got != c.wantDur {
			t.Errorf("%s: durabilityOf = %d, want %d", c.name, got, c.wantDur)
		}
		if got := sessionBare(c.sess); got != c.wantBare {
			t.Errorf("%s: sessionBare = %v, want %v", c.name, got, c.wantBare)
		}
	}
}

func TestDurabilityAction(t *testing.T) {
	cases := []struct {
		name      string
		sess      *model.Session
		wantSheet bool
		wantMsg   string
	}{
		{"bare opens sheet", &model.Session{Kind: model.KindSupervisorInteractive}, true, ""},
		{"bg loose", &model.Session{Kind: model.KindSupervisorBG}, false,
			"already durable — background sessions survive terminal loss · o opens it in tmux"},
		{"bg in tmux", &model.Session{Kind: model.KindSupervisorBG, TmuxTarget: "dev:1.2"}, false,
			"already durable — daemon-owned and visible in tmux (dev:1.2)"},
		{"interactive in tmux", &model.Session{Kind: model.KindSupervisorInteractive, TmuxTarget: "dev:1.2"}, false,
			"already durable — lives in tmux (dev:1.2)"},
		{"tmux interactive", &model.Session{Kind: model.KindTmuxInteractive, TmuxTarget: "dev:1.2"}, false,
			"already durable — lives in tmux (dev:1.2)"},
	}
	for _, c := range cases {
		msg, sheet := durabilityAction(c.sess)
		if sheet != c.wantSheet {
			t.Errorf("%s: openSheet = %v, want %v", c.name, sheet, c.wantSheet)
		}
		if msg != c.wantMsg {
			t.Errorf("%s: msg = %q, want %q", c.name, msg, c.wantMsg)
		}
	}
}

func TestMakeDurableSheet_ExactCopyAndWidth(t *testing.T) {
	lines := makeDurableSheet(&model.Session{Kind: model.KindSupervisorInteractive, Name: "auth-flow refactor"})
	joined := stripANSI(strings.Join(lines, "\n"))
	for _, want := range []string{
		"Make durable — auth-flow refactor",
		"This session runs in a bare terminal.",
		"t   continue in tmux  (recommended)",
		"claude --resume",
		"b   background it from the inside",
		"esc cancel",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("sheet missing %q", want)
		}
	}
	// Every line must be the same visible width (no torn box).
	for i, ln := range lines {
		if w := runeLen(stripANSI(ln)); w != sheetWidth {
			t.Errorf("line %d width = %d, want %d: %q", i, w, sheetWidth, stripANSI(ln))
		}
	}
}

// stripANSI removes SGR escape sequences for width/content assertions.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case inEsc:
			// swallow
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
