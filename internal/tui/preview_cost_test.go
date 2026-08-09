package tui

import (
	"strings"
	"testing"

	"github.com/mjraval/clauditor/internal/model"
)

// TestPreviewTitle_Cost: the "<tok> tok · $<cost>" caption fragment
// (docs/MESSAGING.md §4.2) appears in the preview title only when BOTH the
// model's costEnabled flag is set AND the selected session's own
// CostKnown is true — an unpriced/unknown session must stay silent rather
// than showing a misleading number, and the feature must be invisible when
// [usage].track_cost is off even for a session that does carry priced data.
func TestPreviewTitle_Cost(t *testing.T) {
	priced := &model.Session{Key: "sup-a", SessionID: "a", Name: "alpha",
		CostKnown: true, Tokens: 1_200_000, CostMicroUSD: 3_400_000}
	unpriced := &model.Session{Key: "sup-b", SessionID: "b", Name: "beta",
		CostKnown: false, Tokens: 500, CostMicroUSD: 0}

	m := Model{rows: []Row{
		{Kind: RowSession, Session: priced},
		{Kind: RowSession, Session: unpriced},
	}, cursor: 0, costEnabled: true}

	if title := m.previewTitle(); !strings.Contains(title, "1.2M tok") || !strings.Contains(title, "$3.40") {
		t.Errorf("costEnabled + CostKnown session: title = %q, want tokens+cost", title)
	}

	m.cursor = 1
	if title := m.previewTitle(); strings.Contains(title, "tok") || strings.Contains(title, "$") {
		t.Errorf("CostKnown=false session must not show a cost figure: %q", title)
	}

	// Even a priced session stays silent when the feature is off.
	m.cursor = 0
	m.costEnabled = false
	if title := m.previewTitle(); strings.Contains(title, "1.2M") || strings.Contains(title, "$3.40") {
		t.Errorf("cost caption must not render when costEnabled=false: %q", title)
	}
}
