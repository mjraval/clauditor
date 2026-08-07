// Package collect gathers raw fleet data from `claude agents --json`, tmux,
// and git worktrees. Every external command runs with a context timeout and,
// where relevant, an explicit working directory, so tests can substitute
// the fake binaries in test/stubbin via PATH.
package collect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// DefaultTimeout bounds every external command unless a caller narrows it.
const DefaultTimeout = 5 * time.Second

// Runner executes external commands. The single implementation is execRunner;
// tests either use it against test/stubbin or substitute a fake.
type Runner interface {
	// Run executes name with args in dir (empty = inherit) and returns stdout.
	// Non-zero exit returns an *ExitError carrying stderr.
	Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// ExitError reports a non-zero exit with captured stderr for diagnosis.
type ExitError struct {
	Cmd    string
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("%s: exit %d: %s", e.Cmd, e.Code, e.Stderr)
}

type execRunner struct{}

// NewRunner returns the real subprocess runner.
func NewRunner() Runner { return execRunner{} }

func (execRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return stdout.Bytes(), &ExitError{Cmd: name, Code: ee.ExitCode(), Stderr: stderr.String()}
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return stdout.Bytes(), nil
}
