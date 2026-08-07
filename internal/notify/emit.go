package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// FormatText renders one human-readable line:
//
//	[needs_input] repo/branch · "name" · waiting: permission prompt · 4m
func FormatText(e Event) string {
	s := e.Session
	loc := s.Repo
	if s.Worktree != "" {
		loc = s.Repo + "/" + filepath.Base(s.Worktree)
	}
	line := fmt.Sprintf("[%s] %s", e.Type, loc)
	if s.Name != "" {
		line += fmt.Sprintf(" · %q", s.Name)
	}
	if s.WaitingFor != "" {
		line += " · waiting: " + s.WaitingFor
	}
	if s.AgeSeconds > 0 {
		line += " · " + humanAge(s.AgeSeconds)
	}
	return line
}

func humanAge(secs int64) string {
	d := time.Duration(secs) * time.Second
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", secs)
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// WriteJSON emits one JSON object per line.
func WriteJSON(w io.Writer, e Event) error {
	return json.NewEncoder(w).Encode(e)
}

// WriteText emits the human line.
func WriteText(w io.Writer, e Event) error {
	_, err := fmt.Fprintln(w, FormatText(e))
	return err
}

// Exec runs cmd (via the shell, as documented for --exec) with the event in
// CLAUDITOR_* env vars.
func Exec(cmdline string, e Event) error {
	s := e.Session
	cmd := exec.Command("/bin/sh", "-c", cmdline)
	cmd.Env = append(os.Environ(),
		"CLAUDITOR_EVENT="+string(e.Type),
		"CLAUDITOR_SESSION_NAME="+s.Name,
		"CLAUDITOR_REPO="+s.Repo,
		"CLAUDITOR_WAITING_FOR="+s.WaitingFor,
		"CLAUDITOR_STATE="+s.State,
		"CLAUDITOR_ID="+s.ID,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
