package collect

import (
	"context"
	"strings"
	"testing"
)

const porcelainFixture = `worktree /home/u/projects/mono/core-app
HEAD 4e323dc4eab9ca0ad02f786c13bd63d0102c6594
branch refs/heads/main

worktree /home/u/projects/mono/core-app-worktrees/feat-kms
HEAD aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111
branch refs/heads/feat/kms

worktree /home/u/projects/mono/core-app/.claude/worktrees/wt-123
HEAD bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222
detached
`

func TestParseWorktreePorcelain(t *testing.T) {
	wts := ParseWorktreePorcelain([]byte(porcelainFixture))
	if len(wts) != 3 {
		t.Fatalf("got %d worktrees, want 3", len(wts))
	}
	if wts[0].Path != "/home/u/projects/mono/core-app" || wts[0].Branch != "main" {
		t.Errorf("main worktree wrong: %+v", wts[0])
	}
	if wts[1].Branch != "feat/kms" {
		t.Errorf("branch stripping wrong: %q", wts[1].Branch)
	}
	if wts[2].Branch != "" || wts[2].Head == "" {
		t.Errorf("detached worktree wrong: %+v", wts[2])
	}
}

func TestParseWorktreePorcelain_Empty(t *testing.T) {
	if wts := ParseWorktreePorcelain(nil); len(wts) != 0 {
		t.Fatalf("empty input should yield no worktrees, got %d", len(wts))
	}
}

func TestParseWorktreePorcelain_RealCapture(t *testing.T) {
	wts := ParseWorktreePorcelain(readFixture(t, "git_worktree_porcelain.txt"))
	if len(wts) == 0 {
		t.Fatal("real capture parsed to nothing")
	}
	if wts[0].Path == "" || wts[0].Head == "" {
		t.Errorf("real capture first entry incomplete: %+v", wts[0])
	}
}

type scriptRunner struct {
	out map[string][]byte
	err map[string]error
}

func (s scriptRunner) Run(_ context.Context, _, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	if e, ok := s.err[key]; ok {
		return nil, e
	}
	return s.out[key], nil
}

func TestDiscoverReposAuto_DerivesFromCWDs(t *testing.T) {
	const (
		topCmd    = "git rev-parse --path-format=absolute --show-toplevel"
		commonCmd = "git rev-parse --path-format=absolute --git-common-dir"
	)
	// Two agents live in the main checkout, one in a linked worktree; a
	// fourth cwd is not a repo at all (rev-parse fails).
	r := scriptRunner{
		out: map[string][]byte{
			topCmd:    []byte("/home/u/mono/app-wt/feat\n"), // last write wins per key
			commonCmd: []byte("/home/u/mono/app/.git\n"),
		},
		err: map[string]error{},
	}
	// The scriptRunner keys on command+args only (dir is ignored), so all
	// cwds resolve through the same scripted responses: toplevel then common
	// dir → the single main repo /home/u/mono/app. That proves the dedupe.
	c := &GitCollector{Runner: r}
	got := c.DiscoverReposAuto(context.Background(), nil, nil,
		[]string{"/home/u/mono/app", "/home/u/mono/app", "/home/u/mono/app-wt/feat"})
	if len(got) != 1 || got[0] != "/home/u/mono/app" {
		t.Fatalf("got %v, want [/home/u/mono/app]", got)
	}
}

func TestDiscoverReposAuto_ToleratesNonRepoCWD(t *testing.T) {
	const topCmd = "git rev-parse --path-format=absolute --show-toplevel"
	r := scriptRunner{
		out: map[string][]byte{},
		err: map[string]error{topCmd: &ExitError{Cmd: "git", Code: 128, Stderr: "not a git repository"}},
	}
	c := &GitCollector{Runner: r}
	if got := c.DiscoverReposAuto(context.Background(), nil, nil, []string{"/tmp/not-a-repo"}); len(got) != 0 {
		t.Fatalf("non-repo cwd should yield no repos, got %v", got)
	}
}

func TestDiscoverReposAuto_DefersToConfigured(t *testing.T) {
	// With explicit repos configured, agent cwds are ignored entirely: the
	// runner is never asked for a toplevel (configured DiscoverRepos stats the
	// filesystem instead). A nonexistent repo path yields nothing.
	c := &GitCollector{Runner: scriptRunner{out: map[string][]byte{}, err: map[string]error{}}}
	got := c.DiscoverReposAuto(context.Background(), []string{"/no/such/repo"}, nil, []string{"/home/u/mono/app"})
	if len(got) != 0 {
		t.Fatalf("configured mode should not derive from cwds; got %v", got)
	}
}

func TestAheadBehind(t *testing.T) {
	const cmd = "git rev-list --left-right --count @{upstream}...HEAD"
	tests := []struct {
		name         string
		out          string
		fail         bool
		wantA, wantB int
		wantOK       bool
	}{
		{"ahead 3 behind 1", "1\t3\n", false, 3, 1, true},
		{"clean", "0\t0\n", false, 0, 0, true},
		{"no upstream errors out", "", true, 0, 0, false},
		{"garbage output", "wat\n", false, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := scriptRunner{out: map[string][]byte{cmd: []byte(tt.out)}, err: map[string]error{}}
			if tt.fail {
				r.err[cmd] = &ExitError{Cmd: "git", Code: 128, Stderr: "no upstream"}
			}
			c := &GitCollector{Runner: r, AheadBehind: true}
			a, b, ok := c.aheadBehind(context.Background(), "/wt")
			if a != tt.wantA || b != tt.wantB || ok != tt.wantOK {
				t.Errorf("got (%d,%d,%v) want (%d,%d,%v)", a, b, ok, tt.wantA, tt.wantB, tt.wantOK)
			}
		})
	}
}
