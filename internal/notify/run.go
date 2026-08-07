package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/rishi/clauditor/internal/collect"
	"github.com/rishi/clauditor/internal/config"
	"github.com/rishi/clauditor/internal/model"
)

// Options configures a notify run.
type Options struct {
	Stream   bool   // long-running vs --once
	Format   string // "text" | "json"
	ExecCmd  string // run per event when non-empty
	StateDir string // for --once persistence; default config.StateDir()
}

// Run executes `clauditor notify`. In stream mode it polls until ctx is
// done; in once mode it diffs against the persisted previous snapshot.
func Run(ctx context.Context, cfg *config.Config, fleet *collect.Fleet, opts Options) error {
	debounce := time.Duration(cfg.Notify.DebounceSeconds) * time.Second

	collectSnapshot := func() *model.Snapshot {
		d := fleet.Collect(ctx)
		return model.Correlate(model.Inputs{
			Agents: d.Agents, Panes: d.Panes, Procs: d.Procs, Repos: d.Repos,
			Now: time.Now(),
		})
	}

	emit := func(events []Event) error {
		for _, e := range events {
			var err error
			switch {
			case opts.ExecCmd != "":
				err = Exec(opts.ExecCmd, e)
			case opts.Format == "json":
				err = WriteJSON(os.Stdout, e)
			default:
				err = WriteText(os.Stdout, e)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}

	if !opts.Stream {
		return runOnce(collectSnapshot, emit, debounce, opts.StateDir)
	}

	d := NewDiffer(debounce)
	d.Seed(collectSnapshot()) // baseline; no event storm at startup
	interval := time.Duration(cfg.Poll.ClaudeSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		// jitter ±20%
		j := time.Duration((rand.Float64()*0.4 - 0.2) * float64(interval))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval + j):
		}
		if err := emit(d.Diff(collectSnapshot(), time.Now())); err != nil {
			return err
		}
	}
}

// stateFile layout for --once: the previous snapshot's sessions plus the
// debounce ledger.
type onceState struct {
	Sessions []*model.Session     `json:"sessions"`
	LastSent map[string]time.Time `json:"lastSent"`
}

func runOnce(collectSnapshot func() *model.Snapshot, emit func([]Event) error, debounce time.Duration, stateDir string) error {
	if stateDir == "" {
		var err error
		stateDir, err = config.StateDir()
		if err != nil {
			return fmt.Errorf("state dir: %w", err)
		}
	}
	statePath := filepath.Join(stateDir, "notify-state.json")

	d := NewDiffer(debounce)
	if data, err := os.ReadFile(statePath); err == nil {
		var st onceState
		if err := json.Unmarshal(data, &st); err == nil {
			d.Seed(&model.Snapshot{Sessions: st.Sessions})
			if st.LastSent != nil {
				d.lastSent = st.LastSent
			}
		} else {
			slog.Warn("corrupt notify state, starting fresh", "path", statePath, "err", err)
		}
	}

	cur := collectSnapshot()
	events := d.Diff(cur, time.Now())

	// Persist state atomically before emitting, so a crashed emitter can't
	// cause duplicate notifications on the next cron run.
	st := onceState{Sessions: cur.Sessions, LastSent: d.lastSent}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, statePath); err != nil {
		return err
	}
	return emit(events)
}
