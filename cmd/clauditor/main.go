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
	if len(os.Args) < 2 {
		usage()
		return fmt.Errorf("missing subcommand")
	}
	cmd, args := os.Args[1], os.Args[2:]

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "version", "--version", "-v":
		fmt.Println("clauditor", version.Version)
		return nil
	case "notify":
		return cmdNotify(ctx, args)
	case "status":
		return cmdStatus(ctx, args)
	case "serve":
		return cmdServe(ctx, args)
	case "tui":
		return cmdTUI(ctx, args)
	case "dispatch":
		return cmdDispatch(ctx, args)
	case "doctor":
		return cmdDoctor(ctx, args)
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `clauditor — fleet manager for Claude Code sessions

usage: clauditor <command> [flags]

commands:
  status    print the fleet as a grouped table (--json for raw snapshot)
  notify    emit state-change events (--stream | --once)
  serve     run the HTTP daemon (API + WebUI)
  tui       interactive fleet view
  dispatch  start a background session in a repo/worktree
  doctor    check environment prerequisites
  version   print version

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
