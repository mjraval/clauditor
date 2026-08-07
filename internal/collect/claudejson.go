package collect

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// AgentEntry is one element of `claude agents --json`. This is THE schema
// file (SPEC §5.1): every field optional, unknown fields ignored, and a
// corrupt array element must not drop the snapshot. Schema observed on
// Claude Code 2.1.223 — see internal/collect/testdata/ and docs/RESEARCH.md §Q2.
type AgentEntry struct {
	PID        int    `json:"-"`
	ID         string `json:"id"`        // short id, background only
	CWD        string `json:"cwd"`       //
	Kind       string `json:"kind"`      // "interactive" | "background"
	StartedAt  int64  `json:"-"`         // Unix ms
	SessionID  string `json:"sessionId"` // full UUID
	Name       string `json:"name"`      // mutable display text (supervisor renames it)
	Status     string `json:"status"`    // idle|busy|waiting while alive
	State      string `json:"state"`     // background: working|blocked|done|failed|stopped
	WaitingFor string `json:"waitingFor"`
}

// agentEntryWire mirrors AgentEntry with soft numeric types so a single
// mistyped field degrades to its zero value instead of failing the element.
type agentEntryWire struct {
	PID        json.Number `json:"pid"`
	ID         string      `json:"id"`
	CWD        string      `json:"cwd"`
	Kind       string      `json:"kind"`
	StartedAt  json.Number `json:"startedAt"`
	SessionID  string      `json:"sessionId"`
	Name       string      `json:"name"`
	Status     string      `json:"status"`
	State      string      `json:"state"`
	WaitingFor string      `json:"waitingFor"`
}

// ParseAgentsJSON decodes `claude agents --json` output tolerantly.
// Elements that fail to decode are skipped (logged at debug); elements with
// individually mistyped fields keep their well-typed fields.
func ParseAgentsJSON(data []byte) ([]AgentEntry, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]AgentEntry, 0, len(raw))
	for i, el := range raw {
		var w agentEntryWire
		if err := json.Unmarshal(el, &w); err != nil {
			// Field-level type mismatch or garbage element: retry field by
			// field via a map so one bad field doesn't discard the rest.
			w = salvageEntry(el)
			if w == (agentEntryWire{}) {
				slog.Debug("skipping unparseable agents element", "index", i, "err", err)
				continue
			}
		}
		e := AgentEntry{
			ID: w.ID, CWD: w.CWD, Kind: w.Kind, SessionID: w.SessionID,
			Name: w.Name, Status: w.Status, State: w.State, WaitingFor: w.WaitingFor,
		}
		if v, err := w.PID.Int64(); err == nil {
			e.PID = int(v)
		}
		if v, err := w.StartedAt.Int64(); err == nil {
			e.StartedAt = v
		}
		// An entry with no identity at all is noise.
		if e.SessionID == "" && e.ID == "" && e.PID == 0 && e.CWD == "" {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// salvageEntry re-decodes an element into a generic map and keeps every
// field whose type matches expectations, dropping only the mistyped ones.
func salvageEntry(el json.RawMessage) agentEntryWire {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(el, &m); err != nil {
		return agentEntryWire{}
	}
	var w agentEntryWire
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
	w.ID, w.CWD, w.Kind = str("id"), str("cwd"), str("kind")
	w.SessionID, w.Name = str("sessionId"), str("name")
	w.Status, w.State, w.WaitingFor = str("status"), str("state"), str("waitingFor")
	w.PID, w.StartedAt = num("pid"), num("startedAt")
	return w
}

// ClaudeCollector polls `claude agents --json`.
type ClaudeCollector struct {
	Runner Runner
	Bin    string // defaults to "claude"
}

func NewClaudeCollector(r Runner) *ClaudeCollector {
	return &ClaudeCollector{Runner: r, Bin: "claude"}
}

// Agents runs `claude agents --json` (with --all when includeCompleted).
func (c *ClaudeCollector) Agents(ctx context.Context, includeCompleted bool) ([]AgentEntry, error) {
	args := []string{"agents", "--json"}
	if includeCompleted {
		args = []string{"agents", "--all", "--json"}
	}
	out, err := c.Runner.Run(ctx, "", c.bin(), args...)
	if err != nil {
		return nil, err
	}
	return ParseAgentsJSON(out)
}

// Logs fetches a background session's recent terminal output (raw ANSI
// replay on current versions — the caller caps and strips). Never called
// on the poll loop.
func (c *ClaudeCollector) Logs(ctx context.Context, id string, maxBytes int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := c.Runner.Run(ctx, "", c.bin(), "logs", id)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && len(out) > maxBytes {
		out = out[len(out)-maxBytes:]
	}
	return out, nil
}

func (c *ClaudeCollector) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "claude"
}
