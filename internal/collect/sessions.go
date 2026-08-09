package collect

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
)

// SessionReg is one entry from Claude Code's on-disk session registry
// (`${CLAUDE_CONFIG_DIR:-~/.claude}/sessions/<pid>.json`, one file per live
// session — see docs/MESSAGING.md §1). It is SAFE TO READ ONLY: clauditor
// never opens messagingSocketPath, it only reports whether one is present.
// Every field optional; tolerant parsing mirrors the agents-json discipline
// (internal/collect/claudejson.go) since these files are written live and
// may be read mid-write.
type SessionReg struct {
	PID                 int    `json:"-"`
	SessionID           string `json:"sessionId"`
	CWD                 string `json:"cwd"`
	Kind                string `json:"kind"`
	Name                string `json:"name"`
	Status              string `json:"status"`
	PeerProtocol        int    `json:"-"`
	MessagingSocketPath string `json:"messagingSocketPath"`
}

// sessionRegWire mirrors SessionReg with soft numeric types and a nullable
// socket path, so one mistyped/null field degrades to its zero value
// instead of failing the whole file.
type sessionRegWire struct {
	PID                 json.Number `json:"pid"`
	SessionID           string      `json:"sessionId"`
	CWD                 string      `json:"cwd"`
	Kind                string      `json:"kind"`
	Name                string      `json:"name"`
	Status              string      `json:"status"`
	PeerProtocol        json.Number `json:"peerProtocol"`
	MessagingSocketPath *string     `json:"messagingSocketPath"`
}

// ParseSessionRegistry decodes ONE session-registry file's contents. Unlike
// `claude agents --json`, each file on disk holds a single session object,
// not an array (internal/collect/testdata/sessions_registry_v2.1.226.json
// aggregates several into an array only for table-driven parser testing).
// A corrupt or partial file (the registry is written live and may be caught
// mid-write) yields (SessionReg{}, false) rather than an error — callers
// skip it, exactly like a corrupt agents-json element.
func ParseSessionRegistry(data []byte) (SessionReg, bool) {
	var w sessionRegWire
	if err := json.Unmarshal(data, &w); err != nil {
		// Field-level type mismatch or garbage: retry field by field via a
		// map so one bad field doesn't discard the rest (mirrors
		// salvageEntry in claudejson.go).
		var ok bool
		w, ok = salvageSessionReg(data)
		if !ok {
			return SessionReg{}, false
		}
	}
	// Identity-less entries are dangerous downstream (would collide on
	// synthetic keys) — same discipline as ParseAgentsJSON.
	if w.SessionID == "" {
		return SessionReg{}, false
	}
	reg := SessionReg{
		SessionID: w.SessionID,
		CWD:       w.CWD,
		Kind:      w.Kind,
		Name:      w.Name,
		Status:    w.Status,
	}
	if v, err := w.PID.Int64(); err == nil {
		reg.PID = int(v)
	}
	if v, err := w.PeerProtocol.Int64(); err == nil {
		reg.PeerProtocol = int(v)
	}
	if w.MessagingSocketPath != nil {
		reg.MessagingSocketPath = *w.MessagingSocketPath
	}
	return reg, true
}

// salvageSessionReg re-decodes a registry payload into a generic map and
// keeps every field whose type matches expectations, dropping only the
// mistyped ones. Returns ok=false only when the payload isn't even a JSON
// object (e.g. a mid-write truncation) — mirrors salvageEntry.
func salvageSessionReg(data []byte) (sessionRegWire, bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return sessionRegWire{}, false
	}
	var w sessionRegWire
	str := func(k string) string {
		var s string
		if raw, ok := m[k]; ok && json.Unmarshal(raw, &s) == nil {
			return s
		}
		return ""
	}
	num := func(k string) json.Number {
		var n json.Number
		if raw, ok := m[k]; ok && json.Unmarshal(raw, &n) == nil {
			return n
		}
		return ""
	}
	w.SessionID, w.CWD, w.Kind = str("sessionId"), str("cwd"), str("kind")
	w.Name, w.Status = str("name"), str("status")
	w.PID, w.PeerProtocol = num("pid"), num("peerProtocol")
	if raw, ok := m["messagingSocketPath"]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			w.MessagingSocketPath = &s
		}
	}
	return w, true
}

// SessionsCollector reads Claude Code's live session registry for
// presence/messaging-reachability enrichment (docs/MESSAGING.md §4.1).
// Read-only, best-effort: it is never a state authority — the supervisor
// (`claude agents --json`, ClaudeCollector) stays authoritative for
// working/blocked/done state. A missing registry directory is not an
// error (older Claude Code versions, or messaging disabled).
type SessionsCollector struct{}

// NewSessionsCollector constructs a SessionsCollector.
func NewSessionsCollector() *SessionsCollector { return &SessionsCollector{} }

// Registry globs the session-registry directory and tolerantly parses every
// file: a corrupt/partial file (written live, possibly mid-write) is
// skipped (logged at debug), never fatal to the read.
func (c *SessionsCollector) Registry() ([]SessionReg, error) {
	matches, err := filepath.Glob(filepath.Join(sessionsDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	out := make([]SessionReg, 0, len(matches))
	for _, path := range matches {
		data, rerr := os.ReadFile(path) //nolint:gosec // path from a controlled glob under the config dir
		if rerr != nil {
			slog.Debug("skipping unreadable session registry file", "path", path, "err", rerr)
			continue
		}
		reg, ok := ParseSessionRegistry(data)
		if !ok {
			slog.Debug("skipping unparseable session registry file", "path", path)
			continue
		}
		out = append(out, reg)
	}
	return out, nil
}

// sessionsDir is ${CLAUDE_CONFIG_DIR:-~/.claude}/sessions, mirroring
// internal/transcript's projectsDir handling.
func sessionsDir() string {
	base := os.Getenv("CLAUDE_CONFIG_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "sessions" // best-effort; Glob simply finds nothing
		}
		base = filepath.Join(home, ".claude")
	}
	return filepath.Join(base, "sessions")
}
