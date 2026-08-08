package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mjraval/clauditor/internal/model"
)

// keyModel builds a list-mode Model over the shared fixture snapshot with its
// rows already built, for driving handleListKey in tests.
func keyModel() Model {
	m := Model{cursor: -1}
	m.snap = fixtureSnapshot()
	m.rebuildRows()
	return m
}

func press(m Model, s string) Model {
	next, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(Model)
}

func pressKey(m Model, t tea.KeyType) Model {
	next, _ := m.handleListKey(tea.KeyMsg{Type: t})
	return next.(Model)
}

func TestDirectStateFilters_ToggleAndClear(t *testing.T) {
	m := keyModel()
	m = press(m, "1")
	if m.stateFilter != FilterNeeds {
		t.Fatalf("after 1: filter = %v, want FilterNeeds", m.stateFilter)
	}
	m = press(m, "1") // same key clears
	if m.stateFilter != FilterAll {
		t.Fatalf("after 1 again: filter = %v, want FilterAll", m.stateFilter)
	}
	m = press(m, "2")
	if m.stateFilter != FilterWorking {
		t.Fatalf("after 2: filter = %v, want FilterWorking", m.stateFilter)
	}
	m = press(m, "3") // different key switches, does not clear
	if m.stateFilter != FilterIdle {
		t.Fatalf("after 3: filter = %v, want FilterIdle", m.stateFilter)
	}
	m = press(m, "4")
	if m.stateFilter != FilterTerminal {
		t.Fatalf("after 4: filter = %v, want FilterTerminal", m.stateFilter)
	}
}

func TestEscClearChain(t *testing.T) {
	m := keyModel()
	m.query = "kms"
	m.stateFilter = FilterNeeds

	// esc #1 clears the text filter first.
	m = pressKey(m, tea.KeyEsc)
	if m.query != "" {
		t.Fatalf("esc #1 should clear query, got %q", m.query)
	}
	if m.stateFilter != FilterNeeds {
		t.Fatalf("esc #1 should NOT touch the state filter, got %v", m.stateFilter)
	}
	// esc #2 clears the state filter.
	m = pressKey(m, tea.KeyEsc)
	if m.stateFilter != FilterAll {
		t.Fatalf("esc #2 should clear state filter, got %v", m.stateFilter)
	}
	// esc #3 is a no-op and NEVER quits.
	m = pressKey(m, tea.KeyEsc)
	if m.quitting {
		t.Fatal("esc must never quit")
	}
}

func TestReservedKeysAreNoOps(t *testing.T) {
	for _, k := range []string{"n", "N", "h", "i", ":"} {
		m := keyModel()
		before := m
		m = press(m, k)
		if m.mode != modeList || m.quitting || m.stateFilter != before.stateFilter || m.query != before.query {
			t.Errorf("reserved key %q changed state: mode=%v quitting=%v", k, m.mode, m.quitting)
		}
	}
}

func TestDKeyOpensSheetForBareOnly(t *testing.T) {
	m := Model{cursor: -1}
	bare := &model.Session{Key: "b1", Kind: model.KindSupervisorInteractive, State: model.StateIdle, Name: "bare-one"}
	repo := &model.Repo{Name: "r", Path: "/r", Worktrees: []*model.Worktree{
		{Path: "/r", Branch: "main", Sessions: []*model.Session{bare}},
	}}
	m.snap = &model.Snapshot{Repos: []*model.Repo{repo}, Sessions: []*model.Session{bare}}
	m.rebuildRows()
	m.cursor = FirstSelectable(m.rows)

	m = press(m, "D")
	if m.mode != modeDurable {
		t.Fatalf("D on a bare session should open the sheet (modeDurable), got mode %v", m.mode)
	}
	if m.durableSess != bare {
		t.Fatalf("durableSess not set to the bare session")
	}
	// t continues in tmux and returns to the list.
	next, _ := m.handleDurableKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if next.(Model).mode != modeList {
		t.Fatalf("t should return to list mode")
	}
}

func TestGGAndNav(t *testing.T) {
	m := keyModel()
	m.cursor = FirstSelectable(m.rows)
	last := PrevSelectable(m.rows, 0)

	m = press(m, "G")
	if m.cursor != last {
		t.Fatalf("G should jump to last selectable (%d), got %d", last, m.cursor)
	}
	m = press(m, "g")
	if m.cursor != FirstSelectable(m.rows) {
		t.Fatalf("g should jump to first selectable, got %d", m.cursor)
	}
}
