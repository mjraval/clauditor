package main

import (
	"context"
	"fmt"
)

// Implemented in later milestones (M6–M7); kept as explicit stubs so the
// CLI surface is stable.

func cmdTUI(ctx context.Context, args []string) error {
	return fmt.Errorf("tui: not implemented yet (M6)")
}

func cmdDoctor(ctx context.Context, args []string) error {
	return fmt.Errorf("doctor: not implemented yet (M7)")
}
