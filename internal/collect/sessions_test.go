package collect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestParseSessionRegistry_Fixture re-splits the aggregated array fixture
// (captured for parser testing — real files on disk are one object each,
// see docs/MESSAGING.md §1) into individual per-file payloads and checks
// each parses with the expected fields, including the peer-reachable one
// that carries a non-null messagingSocketPath.
func TestParseSessionRegistry_Fixture(t *testing.T) {
	data := readFixture(t, "sessions_registry_v2.1.226.json")
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("fixture must be a JSON array: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("fixture is empty")
	}

	var sawReachable, sawUnreachable bool
	for i, el := range raw {
		reg, ok := ParseSessionRegistry(el)
		if !ok {
			t.Fatalf("element %d: expected ok=true, got false for %s", i, el)
		}
		if reg.SessionID == "" {
			t.Errorf("element %d: SessionID empty", i)
		}
		if reg.PID == 0 {
			t.Errorf("element %d: PID not parsed: %+v", i, reg)
		}
		switch reg.SessionID {
		case "0ce5be82-6a41-4752-acd7-f00a36f9a0a8":
			sawReachable = true
			if reg.MessagingSocketPath != "/run/user/1000/cc-socks/2684716.sock" {
				t.Errorf("socket path = %q", reg.MessagingSocketPath)
			}
			if reg.PeerProtocol != 1 {
				t.Errorf("peerProtocol = %d, want 1", reg.PeerProtocol)
			}
			if reg.Kind != "bg" || reg.CWD != "/home/rishi/projects/monorepo/stables-core-app" {
				t.Errorf("kind/cwd mismatch: %+v", reg)
			}
		case "9640f722-9992-4d18-91b1-3a9d272c36d2":
			sawUnreachable = true
			// messagingSocketPath is JSON null in the fixture for this entry.
			if reg.MessagingSocketPath != "" {
				t.Errorf("null socket path should decode to empty string, got %q", reg.MessagingSocketPath)
			}
		}
	}
	if !sawReachable {
		t.Error("fixture's socket-bearing entry never matched")
	}
	if !sawUnreachable {
		t.Error("fixture's null-socket entry never matched")
	}
}

func TestParseSessionRegistry_Corrupt(t *testing.T) {
	if _, ok := ParseSessionRegistry([]byte(`{"sessionId": "abc", "pid": `)); ok {
		t.Error("truncated JSON (mid-write) must yield ok=false")
	}
	if _, ok := ParseSessionRegistry([]byte(``)); ok {
		t.Error("empty file must yield ok=false")
	}
	if _, ok := ParseSessionRegistry([]byte(`not json at all`)); ok {
		t.Error("garbage must yield ok=false")
	}
}

// TestParseSessionRegistry_DropsIdentityless mirrors the agents-json
// discipline: a session with no sessionId is dangerous downstream (would
// collide on synthetic keys) so it must be dropped, not defaulted.
func TestParseSessionRegistry_DropsIdentityless(t *testing.T) {
	if _, ok := ParseSessionRegistry([]byte(`{"pid": 123, "cwd": "/tmp"}`)); ok {
		t.Error("entry with no sessionId must yield ok=false")
	}
}

// TestParseSessionRegistry_MistypedFields checks a field-level type
// mismatch degrades that field to zero value without failing the whole
// object (same tolerance as ParseAgentsJSON's salvage path, applied here
// via json.Number/pointer soft-typing rather than a salvage map, since a
// single-object file has no "other elements" to protect).
func TestParseSessionRegistry_MistypedFields(t *testing.T) {
	reg, ok := ParseSessionRegistry([]byte(`{"sessionId":"good-id","pid":"not-a-number","peerProtocol":"also-bad"}`))
	if !ok {
		t.Fatal("mistyped numeric fields must not fail the whole object")
	}
	if reg.SessionID != "good-id" {
		t.Errorf("SessionID = %q", reg.SessionID)
	}
	if reg.PID != 0 || reg.PeerProtocol != 0 {
		t.Errorf("mistyped numerics should degrade to zero, got PID=%d PeerProtocol=%d", reg.PID, reg.PeerProtocol)
	}
}

// TestSessionsCollector_Registry exercises the directory reader against a
// synthetic ~/.claude/sessions dir (via CLAUDE_CONFIG_DIR): a good file, a
// corrupt/partial file (simulating a mid-write catch), and a non-.json file
// that must be ignored by the glob.
func TestSessionsCollector_Registry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	good := `{"pid":111,"sessionId":"sess-good","cwd":"/repo","kind":"interactive","status":"idle","peerProtocol":1,"messagingSocketPath":"/run/user/1000/cc-socks/111.sock"}`
	if err := os.WriteFile(filepath.Join(sessDir, "111.json"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	corrupt := `{"pid":222,"sessionId":"sess-corrupt","cwd":"/re` // truncated mid-write
	if err := os.WriteFile(filepath.Join(sessDir, "222.json"), []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "README.txt"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := NewSessionsCollector()
	regs, err := c.Registry()
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	if len(regs) != 1 {
		t.Fatalf("got %d registrations, want 1 (corrupt+non-json must be skipped): %+v", len(regs), regs)
	}
	if regs[0].SessionID != "sess-good" || regs[0].PID != 111 {
		t.Errorf("unexpected entry: %+v", regs[0])
	}
	if regs[0].MessagingSocketPath == "" || regs[0].PeerProtocol != 1 {
		t.Errorf("expected reachable socket fields: %+v", regs[0])
	}
}

// TestSessionsCollector_Registry_MissingDir: an absent sessions dir (older
// Claude Code, or messaging disabled) is not an error — the registry is
// optional enrichment.
func TestSessionsCollector_Registry_MissingDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // sessions/ subdir never created
	c := NewSessionsCollector()
	regs, err := c.Registry()
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(regs) != 0 {
		t.Errorf("expected no registrations, got %+v", regs)
	}
}
