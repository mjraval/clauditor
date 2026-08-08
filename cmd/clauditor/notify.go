package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/mjraval/clauditor/internal/notify"
)

func cmdNotify(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	load := commonFlags(fs)
	stream := fs.Bool("stream", false, "long-running: emit an event per state transition")
	once := fs.Bool("once", false, "single diff against persisted state (for cron)")
	format := fs.String("format", "text", "output format: text|json")
	execCmd := fs.String("exec", "", "run CMD per event with CLAUDITOR_* env vars")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stream == *once {
		return fmt.Errorf("notify: exactly one of --stream or --once is required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("notify: unknown --format %q (text|json)", *format)
	}
	cfg, err := load()
	if err != nil {
		return err
	}
	fleet := newFleet(cfg, true) // --all: completed sessions produce `completed` events
	return notify.Run(ctx, cfg, fleet, notify.Options{
		Stream:  *stream,
		Format:  *format,
		ExecCmd: *execCmd,
	})
}
