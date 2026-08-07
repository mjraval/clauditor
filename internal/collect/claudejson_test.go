package collect

import (
	"os"
	"path/filepath"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return data
}

func TestParseAgentsJSON_Fixtures(t *testing.T) {
	// Table-driven over every captured schema version (SPEC §16). Add new
	// fixture files here after `scripts/capture-fixtures.sh` on upgrades.
	tests := []struct {
		fixture      string
		wantMin      int // at least this many entries survive
		wantKinds    map[string]bool
		wantBGStates map[string]bool // states that must appear
	}{
		{
			fixture:   "agents_v2.1.223.json",
			wantMin:   1,
			wantKinds: map[string]bool{"interactive": true},
		},
		{
			fixture:      "agents_all_v2.1.223.json",
			wantMin:      2,
			wantKinds:    map[string]bool{"interactive": true, "background": true},
			wantBGStates: map[string]bool{"done": true, "stopped": true},
		},
		{
			fixture:      "agents_bg_states_v2.1.223.json",
			wantMin:      6,
			wantKinds:    map[string]bool{"interactive": true, "background": true},
			wantBGStates: map[string]bool{"working": true, "blocked": true, "done": true, "stopped": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			entries, err := ParseAgentsJSON(readFixture(t, tt.fixture))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(entries) < tt.wantMin {
				t.Fatalf("got %d entries, want >= %d", len(entries), tt.wantMin)
			}
			kinds := map[string]bool{}
			states := map[string]bool{}
			for _, e := range entries {
				kinds[e.Kind] = true
				if e.State != "" {
					states[e.State] = true
				}
				if e.SessionID == "" && e.ID == "" {
					t.Errorf("entry with no identity survived: %+v", e)
				}
			}
			for k := range tt.wantKinds {
				if !kinds[k] {
					t.Errorf("kind %q missing", k)
				}
			}
			for s := range tt.wantBGStates {
				if !states[s] {
					t.Errorf("state %q missing", s)
				}
			}
		})
	}
}

func TestParseAgentsJSON_FieldMapping(t *testing.T) {
	entries, err := ParseAgentsJSON(readFixture(t, "agents_bg_states_v2.1.223.json"))
	if err != nil {
		t.Fatal(err)
	}
	var blocked *AgentEntry
	for i := range entries {
		if entries[i].State == "blocked" && entries[i].WaitingFor != "" {
			blocked = &entries[i]
			break
		}
	}
	if blocked == nil {
		t.Fatal("no blocked+waitingFor entry in fixture")
	}
	if blocked.WaitingFor != "input needed" {
		t.Errorf("waitingFor = %q", blocked.WaitingFor)
	}
	if blocked.ID != "f290fb7a" || blocked.PID != 3526257 {
		t.Errorf("id/pid mapping wrong: %+v", blocked)
	}
	if blocked.StartedAt != 1786043618461 {
		t.Errorf("startedAt = %d", blocked.StartedAt)
	}
	if blocked.Status != "waiting" {
		t.Errorf("status = %q", blocked.Status)
	}
}

func TestParseAgentsJSON_Mangled(t *testing.T) {
	// The mangled fixture has: unknown fields, an element with only 2 fields,
	// an element with every field mistyped, and a good element. Tolerant
	// parsing must keep the good ones and never error (SPEC §5.1).
	entries, err := ParseAgentsJSON(readFixture(t, "agents_mangled.json"))
	if err != nil {
		t.Fatalf("mangled fixture must not fail wholesale: %v", err)
	}
	var byName = map[string]bool{}
	for _, e := range entries {
		byName[e.Name] = true
	}
	if !byName["ok-session"] || !byName["ok-bg-session"] {
		t.Errorf("good entries lost: %+v", entries)
	}
	// The all-mistyped element: strings survive where typed correctly (state),
	// mistyped fields degrade to zero values, and the element is kept because
	// it still has a state — but must not panic or poison others.
	for _, e := range entries {
		if e.Name == "ok-bg-session" {
			if e.State != "blocked" || e.WaitingFor != "permission prompt" || e.ID != "deadbeef" {
				t.Errorf("good bg entry mangled: %+v", e)
			}
		}
	}
}

func TestParseAgentsJSON_NotArray(t *testing.T) {
	if _, err := ParseAgentsJSON([]byte(`{"error":"nope"}`)); err == nil {
		t.Fatal("non-array input must error (collector-level failure, not empty fleet)")
	}
}
