package model

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rishi/clauditor/internal/collect"
)

// Inputs carries one cycle's raw collector output into correlation.
type Inputs struct {
	Agents []collect.AgentEntry
	Panes  []collect.Pane
	Procs  []collect.Proc
	Repos  []collect.RepoInfo
	Now    time.Time
}

// Correlate builds a Snapshot from raw collector data (SPEC §5.4):
//   - supervisor sessions bind to worktrees/repos by cwd prefix match
//   - tmux panes running claude that the supervisor doesn't list become
//     tmux-interactive sessions
//   - a supervisor session visible in a pane is deduped: one Session with
//     TmuxTarget filled (matched by pid subtree, falling back to cwd)
//
// Version stamping is the store's job; Correlate leaves Version zero.
func Correlate(in Inputs) *Snapshot {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	tree := collect.NewPIDTree(in.Procs)

	// --- sessions from the supervisor -----------------------------------
	var sessions []*Session
	claimedPanes := map[string]bool{} // pane id -> consumed by a supervisor session
	for _, a := range in.Agents {
		kind := KindSupervisorInteractive
		if a.Kind == "background" {
			kind = KindSupervisorBG
		}
		state := a.State
		if state == "" {
			// Interactive sessions have no state; status busy|idle|waiting maps coarsely.
			switch a.Status {
			case "busy":
				state = StateWorking
			case "waiting":
				state = StateBlocked
			default:
				state = StateUnknown
			}
		}
		s := &Session{
			Key:        KeyFor(kind, a.SessionID, "", a.PID),
			Kind:       kind,
			ID:         a.ID,
			SessionID:  a.SessionID,
			Name:       a.Name,
			State:      state,
			Status:     a.Status,
			WaitingFor: a.WaitingFor,
			CWD:        a.CWD,
			PID:        a.PID,
		}
		if a.StartedAt > 0 {
			s.StartedAt = time.UnixMilli(a.StartedAt)
			s.AgeSeconds = int64(now.Sub(s.StartedAt).Seconds())
		}
		// Dedupe: find the pane whose pid subtree contains this session's pid.
		if a.PID > 0 {
			for _, p := range in.Panes {
				if p.PanePID == 0 || claimedPanes[p.PaneID] {
					continue
				}
				if tree.ContainsPID(p.PanePID, a.PID) {
					s.TmuxTarget = p.Target()
					s.TmuxPaneID = p.PaneID
					claimedPanes[p.PaneID] = true
					break
				}
			}
		}
		sessions = append(sessions, s)
	}

	// --- tmux-only interactive claude sessions --------------------------
	// cwd fallback set for dedupe: only LIVE supervisor sessions (pid>0)
	// count — a done/stopped session in the same directory must not
	// suppress a live claude found in a pane.
	supervisorCWDs := map[string]bool{}
	for _, a := range in.Agents {
		if a.PID > 0 {
			supervisorCWDs[filepath.Clean(a.CWD)] = true
		}
	}
	for _, p := range in.Panes {
		if claimedPanes[p.PaneID] || p.PanePID == 0 {
			continue
		}
		claudePID, ok := tree.FindClaudeDescendant(p.PanePID)
		if !ok {
			continue
		}
		// The supervisor may know this session but we failed the pid match
		// (e.g. ps raced). Suppress the duplicate by cwd+claude match.
		if supervisorCWDs[filepath.Clean(p.CurrentPath)] {
			continue
		}
		sessions = append(sessions, &Session{
			Key:        KeyFor(KindTmuxInteractive, "", p.PaneID, claudePID),
			Kind:       KindTmuxInteractive,
			State:      StateUnknown,
			CWD:        p.CurrentPath,
			PID:        claudePID,
			TmuxTarget: p.Target(),
			TmuxPaneID: p.PaneID,
			Name:       p.WindowName,
		})
	}

	// --- bind sessions to repos/worktrees -------------------------------
	repos := buildRepos(in.Repos)
	// Longest-prefix wins so a worktree under a repo beats the repo root.
	type binding struct {
		repo *Repo
		wt   *Worktree
	}
	var binds []binding
	for _, r := range repos {
		for _, wt := range r.Worktrees {
			binds = append(binds, binding{repo: r, wt: wt})
		}
	}
	sort.Slice(binds, func(i, j int) bool {
		return len(binds[i].wt.Path) > len(binds[j].wt.Path)
	})

	looseRepo := &Repo{Name: LooseRepoName}
	looseWT := &Worktree{Dirty: "unknown", ManagedBy: ManagedByUser, Sessions: []*Session{}}
	looseRepo.Worktrees = []*Worktree{looseWT}

	for _, s := range sessions {
		bound := false
		cwd := filepath.Clean(s.CWD)
		for _, b := range binds {
			if pathHasPrefix(cwd, b.wt.Path) {
				s.Repo = b.repo.Name
				s.Worktree = b.wt.Path
				b.wt.Sessions = append(b.wt.Sessions, s)
				bound = true
				break
			}
		}
		if !bound {
			s.Repo = LooseRepoName
			looseWT.Sessions = append(looseWT.Sessions, s)
		}
	}
	if len(looseWT.Sessions) > 0 {
		repos = append(repos, looseRepo)
	}

	for _, r := range repos {
		for _, wt := range r.Worktrees {
			SortSessions(wt.Sessions)
		}
	}
	SortSessions(sessions)

	return &Snapshot{
		GeneratedAt: now,
		Repos:       repos,
		Sessions:    sessions,
	}
}

func buildRepos(infos []collect.RepoInfo) []*Repo {
	var out []*Repo
	for _, ri := range infos {
		name := ri.Name
		if name == "" {
			name = filepath.Base(ri.Path)
		}
		r := &Repo{Name: name, Path: ri.Path}
		for _, w := range ri.Worktrees {
			r.Worktrees = append(r.Worktrees, &Worktree{
				Path:      w.Path,
				Branch:    w.Branch,
				Head:      w.Head,
				Dirty:     w.Dirty,
				ManagedBy: w.ManagedBy,
				Sessions:  []*Session{},
			})
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// pathHasPrefix reports whether path is base or inside base (component-wise,
// so /a/bc is NOT under /a/b).
func pathHasPrefix(path, base string) bool {
	path, base = filepath.Clean(path), filepath.Clean(base)
	if path == base {
		return true
	}
	return strings.HasPrefix(path, base+string(filepath.Separator))
}
