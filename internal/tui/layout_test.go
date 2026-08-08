package tui

import (
	"strings"
	"testing"

	"github.com/mjraval/clauditor/internal/model"
)

func TestWideLayout_Breakpoint(t *testing.T) {
	cases := map[int]bool{
		80:  false,
		109: false,
		110: true,
		130: true,
	}
	for w, want := range cases {
		if got := wideLayout(w); got != want {
			t.Errorf("wideLayout(%d) = %v, want %v", w, got, want)
		}
	}
}

func TestPreviewSourceKind(t *testing.T) {
	cases := []struct {
		name string
		sess *model.Session
		want previewKind
	}{
		{"nil", nil, previewNone},
		{"pane wins over id", &model.Session{ID: "abc", TmuxPaneID: "%1"}, previewPane},
		{"id only", &model.Session{ID: "abc"}, previewLogs},
		{"pane only", &model.Session{TmuxPaneID: "%2"}, previewPane},
		{"neither", &model.Session{}, previewNone},
	}
	for _, c := range cases {
		if got := previewSourceKind(c.sess); got != c.want {
			t.Errorf("%s: previewSourceKind = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestReplyEnabled(t *testing.T) {
	cases := []struct {
		name string
		sess *model.Session
		want bool
	}{
		{"nil", nil, false},
		{"blocked with id", &model.Session{State: model.StateBlocked, ID: "abc"}, true},
		{"waiting-for with id", &model.Session{WaitingFor: "approval", ID: "abc"}, true},
		{"blocked no id", &model.Session{State: model.StateBlocked}, false},
		{"working with id", &model.Session{State: model.StateWorking, ID: "abc"}, false},
	}
	for _, c := range cases {
		if got := replyEnabled(c.sess); got != c.want {
			t.Errorf("%s: replyEnabled = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRespawnEnabled(t *testing.T) {
	cases := []struct {
		name string
		sess *model.Session
		want bool
	}{
		{"stopped with id", &model.Session{State: model.StateStopped, ID: "abc"}, true},
		{"failed with id", &model.Session{State: model.StateFailed, ID: "abc"}, true},
		{"stopped no id", &model.Session{State: model.StateStopped}, false},
		{"working with id", &model.Session{State: model.StateWorking, ID: "abc"}, false},
	}
	for _, c := range cases {
		if got := respawnEnabled(c.sess); got != c.want {
			t.Errorf("%s: respawnEnabled = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTmuxSessionName(t *testing.T) {
	if got := tmuxSessionName(&model.Session{TmuxTarget: "alpha:1.2"}); got != "alpha" {
		t.Errorf("tmuxSessionName = %q, want alpha", got)
	}
	if got := tmuxSessionName(&model.Session{TmuxTarget: "solo"}); got != "solo" {
		t.Errorf("tmuxSessionName no-colon = %q, want solo", got)
	}
}

func TestLastNLines(t *testing.T) {
	in := "a\nb\nc\nd\ne\n"
	if got := lastNLines(in, 2); got != "d\ne" {
		t.Errorf("lastNLines(2) = %q, want %q", got, "d\ne")
	}
	if got := lastNLines(in, 10); got != "a\nb\nc\nd\ne" {
		t.Errorf("lastNLines(10) = %q", got)
	}
	if got := lastNLines(in, 0); got != "" {
		t.Errorf("lastNLines(0) = %q, want empty", got)
	}
	if strings.Contains(lastNLines("only\n", 5), "\n\n") {
		t.Error("trailing newline should not produce a blank line")
	}
}
