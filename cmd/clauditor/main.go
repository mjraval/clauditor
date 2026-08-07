// Command clauditor is a tmux/worktree-native fleet manager for Claude Code
// sessions. Subcommands: serve, notify, status, tui, dispatch, doctor, version.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rishi/clauditor/internal/collect"
	"github.com/rishi/clauditor/internal/config"
	"github.com/rishi/clauditor/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "clauditor:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Cockpit-first: bare `clauditor` (or `clauditor -flags` with no
	// subcommand) launches the interactive cockpit. A leading token that
	// starts with "-" is treated as a flag for the cockpit, not a subcommand.
	if len(os.Args) < 2 {
		return cmdTUI(ctx, nil)
	}
	if strings.HasPrefix(os.Args[1], "-") {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Println("clauditor", version.Version)
			return nil
		case "--help", "-h":
			usage()
			return nil
		}
		// Flags with no subcommand → cockpit, flags passed through.
		return cmdTUI(ctx, os.Args[1:])
	}

	cmd, args := os.Args[1], os.Args[2:]

	switch cmd {
	case "version":
		fmt.Println("clauditor", version.Version)
		return nil
	case "tui", "cockpit":
		return cmdTUI(ctx, args)
	case "notify":
		return cmdNotify(ctx, args)
	case "status":
		return cmdStatus(ctx, args)
	case "serve":
		return cmdServe(ctx, args)
	case "dispatch":
		return cmdDispatch(ctx, args)
	case "doctor":
		return cmdDoctor(ctx, args)
	case "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `clauditor — the cockpit for your Claude Code fleet

usage: clauditor [command] [flags]

  clauditor            launch the cockpit (no config required)

commands:
  tui       launch the cockpit (alias; also the bare default)
  status    print the fleet as a grouped table (--json for raw snapshot)
  notify    emit state-change events (--stream | --once)
  dispatch  start a background session in a repo/worktree
  doctor    check environment prerequisites
  version   print version

advanced:
  serve     run the HTTP daemon (API + WebUI) for phone access + notifications

Global flags on every command: --config PATH, --log-level LEVEL
`)
}

// commonFlags registers flags shared by all subcommands and returns a
// loader that must be called after fs.Parse.
func commonFlags(fs *flag.FlagSet) func() (*config.Config, error) {
	cfgPath := fs.String("config", "", "config file path (default: ~/.config/clauditor/config.toml)")
	logLevel := fs.String("log-level", "warn", "log level: debug|info|warn|error")
	return func() (*config.Config, error) {
		setupLogging(*logLevel)
		return config.Load(*cfgPath)
	}
}

func setupLogging(level string) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelWarn
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
}

// newFleet wires the collectors from config.
func newFleet(cfg *config.Config, includeAll bool) *collect.Fleet {
	r := collect.NewRunner()
	git := collect.NewGitCollector(r)
	git.DirtyCheck = cfg.Git.DirtyCheck
	git.AheadBehind = cfg.Git.AheadBehind
	return &collect.Fleet{
		Claude:        collect.NewClaudeCollector(r),
		Tmux:          collect.NewTmuxCollector(r),
		Git:           git,
		Repos:         cfg.Repos,
		WorkspaceDirs: cfg.WorkspaceDirs,
		IncludeAll:    includeAll,
	}
}
