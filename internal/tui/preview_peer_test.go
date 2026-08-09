package tui

import (
	"strings"
	"testing"

	"github.com/mjraval/clauditor/internal/model"
)

// TestPreviewTitle_PeerReachable: the "⇄ peer-reachable" mention
// (docs/MESSAGING.md §4.1) appears in the preview caption exactly when the
// selected session's PeerReachable is true, and never crowds the row (this
// only touches the detail-preview title, not BuildRows/row badges).
func TestPreviewTitle_PeerReachable(t *testing.T) {
	reachable := &model.Session{Key: "sup-a", SessionID: "a", Name: "alpha", PeerReachable: true}
	notReachable := &model.Session{Key: "sup-b", SessionID: "b", Name: "beta", PeerReachable: false}

	m := Model{rows: []Row{
		{Kind: RowSession, Session: reachable},
		{Kind: RowSession, Session: notReachable},
	}, cursor: 0}

	if title := m.previewTitle(); !strings.Contains(title, "⇄ peer-reachable") {
		t.Errorf("peer-reachable session: title = %q, want it to contain the mention", title)
	}

	m.cursor = 1
	if title := m.previewTitle(); strings.Contains(title, "⇄") {
		t.Errorf("non-reachable session: title = %q, must not mention peer-reachability", title)
	}
}
