package tui

import (
	"testing"

	"github.com/mjraval/clauditor/internal/model"
)

func TestStateFilter_Cycle(t *testing.T) {
	want := []StateFilter{FilterNeeds, FilterWorking, FilterIdle, FilterTerminal, FilterAll}
	f := FilterAll
	for i, w := range want {
		f = f.Next()
		if f != w {
			t.Errorf("step %d: Next() = %v, want %v", i, f, w)
		}
	}
}

func TestStateFilter_Matches(t *testing.T) {
	cases := []struct {
		f      StateFilter
		bucket string
		want   bool
	}{
		{FilterAll, "needs", true},
		{FilterAll, "terminal", true},
		{FilterNeeds, "needs", true},
		{FilterNeeds, "working", false},
		{FilterWorking, "working", true},
		{FilterIdle, "idle", true},
		{FilterIdle, "terminal", false},
		{FilterTerminal, "terminal", true},
	}
	for _, c := range cases {
		if got := c.f.Matches(c.bucket); got != c.want {
			t.Errorf("%v.Matches(%q) = %v, want %v", c.f, c.bucket, got, c.want)
		}
	}
}

func TestStateFilter_LabelAndString(t *testing.T) {
	if FilterAll.String() != "all" || FilterAll.Label() != "all" {
		t.Errorf("FilterAll strings wrong: %q %q", FilterAll.String(), FilterAll.Label())
	}
	if FilterNeeds.String() != "needs" || FilterNeeds.Label() != "needs-input" {
		t.Errorf("FilterNeeds strings wrong: %q %q", FilterNeeds.String(), FilterNeeds.Label())
	}
}

func TestMatchesQuery(t *testing.T) {
	sess := &model.Session{Name: "kms-rotation", State: model.StateWorking, Repo: "alpha", Worktree: "/repos/alpha", WaitingFor: "", ID: "abc123", TmuxTarget: "clauditor:2"}
	row := Row{Kind: RowSession, Session: sess}
	cases := []struct {
		query string
		want  bool
	}{
		{"", true},
		{"KMS", true}, // case-insensitive
		{"rotation", true},
		{"alpha", true},
		{"abc123", true},
		{"clauditor:2", true},
		{"nope-not-here", false},
	}
	for _, c := range cases {
		if got := MatchesQuery(row, c.query); got != c.want {
			t.Errorf("MatchesQuery(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}

func TestMatchesQuery_HeaderRowAlwaysPasses(t *testing.T) {
	if !MatchesQuery(Row{Kind: RowRepo, Text: "alpha"}, "anything") {
		t.Error("header rows should never be filtered directly (they survive iff a child session matches)")
	}
}
