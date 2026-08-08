package main

import (
	"context"
	"flag"

	"github.com/mjraval/clauditor/internal/tui"
)

// cmdTUI runs the minimal fleet TUI (SPEC §11 / M6). It talks to a running
// `clauditor serve` daemon over loopback HTTP when available, falling back
// to running the collectors in-process otherwise (see internal/tui.Run).
func cmdTUI(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	load := commonFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := load()
	if err != nil {
		return err
	}
	return tui.Run(ctx, cfg)
}
