package actions

import (
	"context"
	"fmt"
	"strings"

	"github.com/mjraval/clauditor/internal/model"
)

// hiddenSession is the tmux session clauditor owns for attach windows.
const hiddenSession = "clauditor"

// Stop runs `claude stop <id>` for a supervisor session.
func (a *Actions) Stop(ctx context.Context, s *model.Session) error {
	if s.ID == "" {
		return errf("bad_target", "session %s has no background id (interactive sessions stop from their terminal)", s.Key)
	}
	_, err := a.run(ctx, "", a.ClaudeBin, "stop", s.ID)
	return err
}

// Respawn runs `claude respawn <id>`.
func (a *Actions) Respawn(ctx context.Context, s *model.Session) error {
	if s.ID == "" {
		return errf("bad_target", "session %s has no background id", s.Key)
	}
	_, err := a.run(ctx, "", a.ClaudeBin, "respawn", s.ID)
	return err
}

// OpenInTmuxResult tells the client where the window landed.
type OpenInTmuxResult struct {
	Target string `json:"target"` // "clauditor:<n>" best effort
	Attach string `json:"attach"` // copy-paste command
}

// OpenInTmux ensures the hidden `clauditor` session exists, then opens a
// window running `claude attach <id>` in the session's cwd (SPEC §7.2).
func (a *Actions) OpenInTmux(ctx context.Context, s *model.Session) (*OpenInTmuxResult, error) {
	if s.ID == "" {
		return nil, errf("bad_target", "session %s has no attachable background id", s.Key)
	}
	if !ValidSessionID(s.ID) {
		return nil, errf("bad_target", "session id has unexpected format")
	}
	// Idempotent session creation: has-session probe, then new-session -d.
	if _, err := a.Runner.Run(ctx, "", a.TmuxBin, "has-session", "-t", hiddenSession); err != nil {
		if _, err := a.run(ctx, "", a.TmuxBin, "new-session", "-d", "-s", hiddenSession); err != nil {
			return nil, err
		}
	}
	name := shortWindowName(s)
	args := []string{"new-window", "-t", hiddenSession, "-n", name, "-P", "-F", "#{session_name}:#{window_index}"}
	if s.CWD != "" {
		args = append(args, "-c", s.CWD)
	}
	args = append(args, fmt.Sprintf("%s attach %s", a.ClaudeBin, s.ID))
	out, err := a.run(ctx, "", a.TmuxBin, args...)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(string(out))
	if target == "" {
		target = hiddenSession
	}
	return &OpenInTmuxResult{
		Target: target,
		Attach: "tmux attach -t " + hiddenSession,
	}, nil
}

func shortWindowName(s *model.Session) string {
	n := s.Name
	if n == "" {
		n = s.ID
	}
	n = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, n)
	if len(n) > 20 {
		n = n[:20]
	}
	if n == "" {
		n = "session"
	}
	return n
}
