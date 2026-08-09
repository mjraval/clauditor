// Package model defines clauditor's unified fleet model and the correlation
// logic that binds Claude sessions to git repos/worktrees and tmux panes.
package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// SessionKind classifies where a session was discovered.
type SessionKind string

const (
	KindSupervisorBG          SessionKind = "supervisor-bg"
	KindSupervisorInteractive SessionKind = "supervisor-interactive"
	KindTmuxInteractive       SessionKind = "tmux-interactive"
)

// Session states. Supervisor-reported states are authoritative; tmux-only
// sessions carry StateUnknown.
const (
	StateWorking = "working"
	StateBlocked = "blocked"
	StateDone    = "done"
	StateFailed  = "failed"
	StateStopped = "stopped"
	StateIdle    = "idle"    // interactive sessions at an empty prompt
	StateUnknown = "unknown" // tmux-only sessions (no supervisor data)
)

// ManagedBy labels who created a worktree.
const (
	ManagedByUser       = "user"
	ManagedByClaudeCode = "claude-code"
)

// LooseRepoName is the synthetic group for sessions whose cwd matches no
// configured repo or worktree.
const LooseRepoName = "(loose)"

// Session is one Claude Code session (background, interactive, or a claude
// process observed in a tmux pane).
type Session struct {
	Key        string      `json:"key"`
	Kind       SessionKind `json:"kind"`
	ID         string      `json:"id,omitempty"`        // short id (attach/logs/stop)
	SessionID  string      `json:"sessionId,omitempty"` // full UUID (claude --resume)
	Name       string      `json:"name,omitempty"`
	State      string      `json:"state"`
	Status     string      `json:"status,omitempty"` // busy|idle|waiting while process alive
	WaitingFor string      `json:"waitingFor,omitempty"`
	StartedAt  time.Time   `json:"startedAt,omitzero"`
	AgeSeconds int64       `json:"ageSeconds"`
	CWD        string      `json:"cwd"`
	Repo       string      `json:"repo,omitempty"`
	Worktree   string      `json:"worktree,omitempty"` // worktree path
	PID        int         `json:"pid,omitempty"`
	TmuxTarget string      `json:"tmuxTarget,omitempty"` // "session:window.pane"
	TmuxPaneID string      `json:"tmuxPaneId,omitempty"` // "%42"
	// PeerReachable is true when Claude Code's session registry
	// (docs/MESSAGING.md §4.1) has this session's SessionID with a live
	// messaging socket + peerProtocol — i.e. it can be reached by another
	// Claude session's SendMessage tool. Enrichment only; never a state
	// authority (see internal/model/enrich.go).
	PeerReachable bool `json:"peerReachable,omitempty"`

	// Tokens/CostMicroUSD/CostKnown are the optional per-session cost
	// readout (docs/MESSAGING.md §4.2): total input+output tokens, cost in
	// microdollars (1e-6 USD — format at the display edge, never as a
	// float dollar amount), and whether every model the session used had a
	// confirmed price. Populated by internal/store.Poller from
	// internal/usage ONLY when [usage].track_cost is enabled — an
	// unpopulated session simply carries the zero values, which renderers
	// must treat as "unknown", not "free" (gate display on CostKnown).
	Tokens       int64 `json:"tokens,omitempty"`
	CostMicroUSD int64 `json:"costMicroUSD,omitempty"`
	CostKnown    bool  `json:"costKnown,omitempty"`
}

// NeedsInput reports whether the session is waiting on a human.
func (s *Session) NeedsInput() bool {
	return s.State == StateBlocked || s.WaitingFor != ""
}

// Worktree is one git worktree (the main checkout is also a worktree).
type Worktree struct {
	Path      string     `json:"path"`
	Branch    string     `json:"branch,omitempty"`
	Head      string     `json:"head,omitempty"`
	Dirty     string     `json:"dirty"` // "true" | "false" | "unknown"
	ManagedBy string     `json:"managedBy"`
	URL       string     `json:"url,omitempty"`
	Ahead     *int       `json:"ahead,omitempty"`  // git.ahead_behind only
	Behind    *int       `json:"behind,omitempty"` // git.ahead_behind only
	Sessions  []*Session `json:"sessions"`
}

// Repo is one git repository with its worktrees.
type Repo struct {
	Name      string      `json:"name"`
	Path      string      `json:"path"`
	Worktrees []*Worktree `json:"worktrees"`
}

// Snapshot is the full correlated fleet state at one instant.
type Snapshot struct {
	Version     uint64     `json:"version"`
	GeneratedAt time.Time  `json:"generatedAt"`
	Repos       []*Repo    `json:"repos"`
	Sessions    []*Session `json:"sessions"` // flat view, same pointers as under repos
	Collectors  Health     `json:"collectors"`
	// CollectorAges is seconds since each collector's last success, keyed
	// "claude"|"tmux"|"git" (-1 = never). Populated by store.Poller on every
	// Set so it serializes over /api/v1/state — the TUI reads it for the `?`
	// sources line and header failure segments. Distinct from Health, which
	// stays internal to /healthz.
	CollectorAges map[string]int64 `json:"collectorAges,omitempty"`
}

// Health reports collector liveness for /healthz.
type Health struct {
	ClaudeLastSuccess time.Time `json:"-"`
	TmuxLastSuccess   time.Time `json:"-"`
	GitLastSuccess    time.Time `json:"-"`
}

// SessionByKey returns the session with the given key, or nil.
func (s *Snapshot) SessionByKey(key string) *Session {
	for _, sess := range s.Sessions {
		if sess.Key == key {
			return sess
		}
	}
	return nil
}

// KeyFor derives a stable synthetic key for a session. Supervisor sessions
// key on their sessionId (stable across restarts); tmux-only sessions key
// on pane id + pid (a new claude process in the same pane is a new session).
// A supervisor entry with a degraded sessionId (schema drift) falls back to
// the short id, then pid, so two degraded entries can never share a key.
func KeyFor(kind SessionKind, sessionID, paneID string, pid int) string {
	switch kind {
	case KindTmuxInteractive:
		return fmt.Sprintf("tmux-%s-%d", strings.TrimPrefix(paneID, "%"), pid)
	default:
		if sessionID != "" {
			return "sup-" + sessionID
		}
		return fmt.Sprintf("sup-pid-%d", pid)
	}
}

// SortSessions orders sessions: needs-input first, then working, then the
// rest; within a group, most recently started first.
func SortSessions(ss []*Session) {
	rank := func(s *Session) int {
		switch {
		case s.NeedsInput():
			return 0
		case s.State == StateWorking:
			return 1
		case s.State == StateIdle, s.State == StateUnknown:
			return 2
		case s.State == StateDone:
			return 3
		case s.State == StateFailed:
			return 4
		default:
			return 5
		}
	}
	sort.SliceStable(ss, func(i, j int) bool {
		ri, rj := rank(ss[i]), rank(ss[j])
		if ri != rj {
			return ri < rj
		}
		return ss[i].StartedAt.After(ss[j].StartedAt)
	})
}
