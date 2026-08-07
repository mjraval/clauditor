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
func KeyFor(kind SessionKind, sessionID, paneID string, pid int) string {
	switch kind {
	case KindTmuxInteractive:
		return fmt.Sprintf("tmux-%s-%d", strings.TrimPrefix(paneID, "%"), pid)
	default:
		return "sup-" + sessionID
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
