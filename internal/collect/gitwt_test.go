package collect

import "testing"

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
