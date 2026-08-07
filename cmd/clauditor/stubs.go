package main

import (
	"context"
	"fmt"
)

// Implemented in later milestones (M2–M7); kept as explicit stubs so the
// CLI surface is stable from M1 on.


func cmdServe(ctx context.Context, args []string) error {
	return fmt.Errorf("serve: not implemented yet (M3)")
}

func cmdTUI(ctx context.Context, args []string) error {
	return fmt.Errorf("tui: not implemented yet (M6)")
}

func cmdDispatch(ctx context.Context, args []string) error {
	return fmt.Errorf("dispatch: not implemented yet (M3)")
}

func cmdDoctor(ctx context.Context, args []string) error {
	return fmt.Errorf("doctor: not implemented yet (M7)")
}
