package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"

	"github.com/rishi/clauditor/internal/api"
	"github.com/rishi/clauditor/internal/store"
	"github.com/rishi/clauditor/web"
)

func cmdServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	load := commonFlags(fs)
	listen := fs.String("listen", "", "listen address (overrides config; default 127.0.0.1:8790)")
	snapshotFile := fs.String("snapshot-file", "", "write the latest snapshot to this JSON file (debug)")
	devInsecure := fs.Bool("dev-insecure-local", false, "DANGEROUS: skip Access JWT validation for loopback requests")
	exposed := fs.Bool("i-know-this-is-exposed", false, "allow binding a non-loopback address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := load()
	if err != nil {
		return err
	}
	if *listen == "" {
		*listen = cfg.Serve.Listen
	}
	if *snapshotFile == "" {
		*snapshotFile = cfg.Serve.SnapshotFile
	}

	st := store.New()
	st.SnapshotFile = *snapshotFile
	fleet := newFleet(cfg, true)
	poller := &store.Poller{Fleet: fleet, Store: st, Cfg: cfg}
	go poller.Run(ctx)

	auth := &api.AuthConfig{
		TeamDomain:       cfg.Access.TeamDomain,
		PolicyAUD:        cfg.Access.PolicyAUD,
		DevInsecureLocal: *devInsecure,
	}
	if cfg.Access.TeamDomain != "" {
		auth.JWKS = api.NewJWKS(cfg.Access.TeamDomain)
	}

	acts := newActions(cfg)
	srv := &api.Server{
		Store:   st,
		Cfg:     cfg,
		Auth:    auth,
		Actions: acts,
		Claude:  fleet.Claude,
		Tmux:    fleet.Tmux,
		WebFS:   web.FS(),
	}

	mode := "read-only"
	if cfg.Actions.Enabled {
		mode = "actions enabled"
	}
	slog.Info("clauditor serve starting", "listen", *listen, "mode", mode)
	fmt.Printf("clauditor serve on http://%s (%s)\n", *listen, mode)
	return srv.ListenAndServe(ctx, *listen, *exposed)
}
