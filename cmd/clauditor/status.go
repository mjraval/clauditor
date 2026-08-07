package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rishi/clauditor/internal/model"
	"github.com/rishi/clauditor/internal/store"
)

func cmdStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	load := commonFlags(fs)
	asJSON := fs.Bool("json", false, "print the raw snapshot as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := load()
	if err != nil {
		return err
	}
	p := &store.Poller{Fleet: newFleet(cfg, true), Store: store.New(), Cfg: cfg}
	snap := p.RunOnce(ctx)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(snap)
	}
	renderStatus(os.Stdout, snap)
	return nil
}

// State glyphs (bold/underline-free terminal fallback; the WebUI adds
// weight/underline for colorblind accessibility).
func glyph(s *model.Session) string {
	switch {
	case s.NeedsInput():
		return "◐"
	case s.State == model.StateWorking:
		return "●"
	case s.State == model.StateDone:
		return "✔"
	case s.State == model.StateFailed:
		return "✕"
	case s.State == model.StateStopped:
		return "⏹"
	default:
		return "○"
	}
}

func renderStatus(w io.Writer, snap *model.Snapshot) {
	if snap == nil || len(snap.Repos) == 0 {
		fmt.Fprintln(w, "no repos configured and no sessions found — check ~/.config/clauditor/config.toml")
		return
	}
	needs, working := 0, 0
	for _, s := range snap.Sessions {
		if s.NeedsInput() {
			needs++
		} else if s.State == model.StateWorking {
			working++
		}
	}
	fmt.Fprintf(w, "clauditor · %d sessions · %d need input · %d working · v%d %s\n\n",
		len(snap.Sessions), needs, working, snap.Version, snap.GeneratedAt.Format("15:04:05"))

	for _, r := range snap.Repos {
		total := 0
		for _, wt := range r.Worktrees {
			total += len(wt.Sessions)
		}
		if total == 0 && r.Name != model.LooseRepoName {
			continue // uninteresting repo rows keep the table short
		}
		fmt.Fprintf(w, "%s  (%s)\n", r.Name, r.Path)
		for _, wt := range r.Worktrees {
			if len(wt.Sessions) == 0 {
				continue
			}
			dirty := ""
			if wt.Dirty == "true" {
				dirty = " ●dirty"
			}
			managed := ""
			if wt.ManagedBy == model.ManagedByClaudeCode {
				managed = " [claude-managed]"
			}
			branch := wt.Branch
			if branch == "" {
				branch = filepath.Base(wt.Path)
			}
			if r.Name == model.LooseRepoName {
				branch = "-"
			}
			fmt.Fprintf(w, "  %s%s%s\n", branch, dirty, managed)
			for _, s := range wt.Sessions {
				fmt.Fprintf(w, "    %s %s\n", glyph(s), sessionLine(s))
			}
		}
		fmt.Fprintln(w)
	}
}

func sessionLine(s *model.Session) string {
	var b strings.Builder
	state := s.State
	if s.WaitingFor != "" {
		state += ": " + s.WaitingFor
	}
	fmt.Fprintf(&b, "%-28s %-24s", truncate(displayName(s), 28), state)
	if s.TmuxTarget != "" {
		fmt.Fprintf(&b, " ⧉ %s", s.TmuxTarget)
	}
	if !s.StartedAt.IsZero() {
		fmt.Fprintf(&b, " · %s", humanDur(time.Duration(s.AgeSeconds)*time.Second))
	}
	if s.ID != "" {
		fmt.Fprintf(&b, " · %s", s.ID)
	}
	return b.String()
}

func displayName(s *model.Session) string {
	if s.Name != "" {
		return s.Name
	}
	if s.Kind == model.KindTmuxInteractive {
		return "(interactive in tmux)"
	}
	return "(unnamed)"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func humanDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
