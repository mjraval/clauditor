package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/mjraval/clauditor/internal/actions"
	"github.com/mjraval/clauditor/internal/collect"
	"github.com/mjraval/clauditor/internal/config"
	"github.com/mjraval/clauditor/internal/store"
)

func newActions(cfg *config.Config) *actions.Actions {
	a := actions.New(collect.NewRunner())
	a.WorktreeBase = cfg.Dispatch.WorktreeBase
	return a
}

func cmdDispatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	load := commonFlags(fs)
	repo := fs.String("repo", "", "target repo name (as shown by `clauditor status`)")
	worktree := fs.String("worktree", "", "existing worktree path within --repo")
	newBranch := fs.String("new-worktree", "", "create a worktree for this branch and dispatch inside it")
	base := fs.String("base", "", "base ref for --new-worktree (default HEAD)")
	cwd := fs.String("cwd", "", "explicit target directory (alternative to --repo)")
	name := fs.String("name", "", "session name")
	mdl := fs.String("model", "", "model override")
	agent := fs.String("agent", "", "agent override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("dispatch: exactly one prompt argument required")
	}
	cfg, err := load()
	if err != nil {
		return err
	}

	// One collection cycle so target resolution sees the fleet.
	p := &store.Poller{Fleet: newFleet(cfg, false), Store: store.New(), Cfg: cfg}
	snap := p.RunOnce(ctx)

	req := actions.DispatchRequest{
		Prompt: fs.Arg(0),
		Name:   *name,
		Model:  *mdl,
		Agent:  *agent,
		Target: actions.DispatchTarget{CWD: *cwd, Repo: *repo, Worktree: *worktree},
	}
	if *newBranch != "" {
		req.Target.NewWorktree = &actions.NewWorktree{Branch: *newBranch, Base: *base}
	}
	res, err := newActions(cfg).Dispatch(ctx, snap, req)
	if err != nil {
		return err
	}
	if res.CreatedWT != "" {
		fmt.Println("created worktree:", res.CreatedWT)
	}
	fmt.Printf("dispatched in %s", res.Dir)
	if res.ShortID != "" {
		fmt.Printf(" · %s · attach with: claude attach %s", res.ShortID, res.ShortID)
	}
	fmt.Println()
	return nil
}
