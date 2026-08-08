package actions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mjraval/clauditor/internal/collect"
	"github.com/mjraval/clauditor/internal/model"
)

// fakeRunner records every invocation and returns scripted output.
type fakeRunner struct {
	calls   []call
	outputs map[string][]byte // key: name + " " + first arg
	errs    map[string]error
}

type call struct {
	dir  string
	name string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{dir: dir, name: name, args: args})
	key := name
	if len(args) > 0 {
		key += " " + args[0]
	}
	if err, ok := f.errs[key]; ok {
		return nil, err
	}
	return f.outputs[key], nil
}

func (f *fakeRunner) argv(i int) string {
	c := f.calls[i]
	return strings.TrimSpace(c.name + " " + strings.Join(c.args, " "))
}

func newFake() *fakeRunner {
	return &fakeRunner{outputs: map[string][]byte{}, errs: map[string]error{}}
}

func snapWithRepo(t *testing.T, repoPath string) *model.Snapshot {
	t.Helper()
	return &model.Snapshot{Repos: []*model.Repo{{
		Name: "alpha", Path: repoPath,
		Worktrees: []*model.Worktree{{Path: repoPath, Branch: "main"}},
	}}}
}

func TestDispatch_ArgvAndDir(t *testing.T) {
	f := newFake()
	f.outputs["claude --bg"] = []byte("backgrounded · abc12345\n")
	a := New(f)
	dir := t.TempDir()

	res, err := a.Dispatch(context.Background(), snapWithRepo(t, dir), DispatchRequest{
		Prompt: "do the thing",
		Name:   "myname",
		Model:  "sonnet",
		Target: DispatchTarget{Repo: "alpha"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ShortID != "abc12345" {
		t.Errorf("shortId = %q", res.ShortID)
	}
	last := f.calls[len(f.calls)-1]
	want := "claude --bg --name myname --model sonnet do the thing"
	if f.argv(len(f.calls)-1) != want {
		t.Errorf("argv = %q, want %q", f.argv(len(f.calls)-1), want)
	}
	if last.dir != dir {
		t.Errorf("dir = %q, want %q", last.dir, dir)
	}
}

func TestDispatch_RejectsBypassFlags(t *testing.T) {
	a := New(newFake())
	for _, req := range []DispatchRequest{
		{Prompt: "x --dangerously-skip-permissions", Target: DispatchTarget{CWD: "/tmp"}},
		{Prompt: "x", Name: "--allow-dangerously-skip-permissions", Target: DispatchTarget{CWD: "/tmp"}},
		{Prompt: "x", Model: "--permission-mode bypassPermissions", Target: DispatchTarget{CWD: "/tmp"}},
		{Prompt: "x", Agent: "bypassPermissions", Target: DispatchTarget{CWD: "/tmp"}},
	} {
		if _, err := a.Dispatch(context.Background(), nil, req); err == nil {
			t.Errorf("bypass flag accepted: %+v", req)
		}
	}
}

func TestDispatch_RejectsFlagShapedValues(t *testing.T) {
	a := New(newFake())
	_, err := a.Dispatch(context.Background(), nil, DispatchRequest{
		Prompt: "x", Name: "--exec", Target: DispatchTarget{CWD: "/tmp"},
	})
	if err == nil {
		t.Error("flag-shaped name accepted")
	}
}

func TestDispatch_NewWorktree_NewBranch(t *testing.T) {
	f := newFake()
	// show-ref fails => branch doesn't exist => -b shape
	f.errs["git show-ref"] = &collect.ExitError{Cmd: "git", Code: 1}
	f.outputs["claude --bg"] = []byte("backgrounded · deadbeef\n")
	a := New(f)
	repo := t.TempDir()

	res, err := a.Dispatch(context.Background(), snapWithRepo(t, repo), DispatchRequest{
		Prompt: "build it",
		Target: DispatchTarget{Repo: "alpha", NewWorktree: &NewWorktree{Branch: "feat/kms-rotation", Base: "main"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-worktrees", "feat-kms-rotation")
	if res.CreatedWT != wantDir || res.Dir != wantDir {
		t.Errorf("dir = %q createdWT = %q, want %q", res.Dir, res.CreatedWT, wantDir)
	}

	var seq []string
	for _, c := range f.calls {
		seq = append(seq, strings.Join(append([]string{c.name}, c.args...), " "))
	}
	joined := strings.Join(seq, "\n")
	for _, want := range []string{
		"git worktree prune",
		"git rev-parse --verify --end-of-options main",
		"git show-ref --verify --quiet refs/heads/feat/kms-rotation",
		"git worktree add " + wantDir + " -b feat/kms-rotation main",
		"claude --bg build it",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing call %q in:\n%s", want, joined)
		}
	}
	// worktree ops run in the repo; dispatch runs in the new worktree
	for _, c := range f.calls {
		if c.name == "git" && c.dir != repo {
			t.Errorf("git call ran in %q, want repo %q", c.dir, repo)
		}
		if c.name == "claude" && c.dir != wantDir {
			t.Errorf("claude ran in %q, want %q", c.dir, wantDir)
		}
	}
}

func TestDispatch_NewWorktree_ExistingBranch(t *testing.T) {
	f := newFake() // show-ref succeeds => existing branch shape
	f.outputs["claude --bg"] = []byte("backgrounded · cafe0001\n")
	a := New(f)
	repo := t.TempDir()

	_, err := a.Dispatch(context.Background(), snapWithRepo(t, repo), DispatchRequest{
		Prompt: "resume it",
		Target: DispatchTarget{Repo: "alpha", NewWorktree: &NewWorktree{Branch: "feat/old"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, c := range f.calls {
		joined += strings.Join(append([]string{c.name}, c.args...), " ") + "\n"
	}
	if !strings.Contains(joined, "git worktree add") || strings.Contains(joined, " -b ") {
		t.Errorf("existing branch must not use -b:\n%s", joined)
	}
}

func TestDispatch_NewWorktree_IdempotentOnExisting(t *testing.T) {
	f := newFake()
	f.outputs["claude --bg"] = []byte("backgrounded · aaaa2222\n")
	a := New(f)
	repo := t.TempDir()
	// Pre-create the worktree dir with a .git file (linked worktree marker).
	wt := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-worktrees", "feat-x")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := a.Dispatch(context.Background(), snapWithRepo(t, repo), DispatchRequest{
		Prompt: "again",
		Target: DispatchTarget{Repo: "alpha", NewWorktree: &NewWorktree{Branch: "feat/x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Dir != wt || res.CreatedWT != "" {
		t.Errorf("idempotent path wrong: %+v", res)
	}
	for _, c := range f.calls {
		if c.name == "git" && len(c.args) > 1 && c.args[0] == "worktree" && c.args[1] == "add" {
			t.Error("worktree add must not run when dir already is a worktree")
		}
	}
}

func TestValidBranchName(t *testing.T) {
	good := []string{"main", "feat/kms", "a-b_c.d", "release/1.2.3"}
	bad := []string{"", "-x", "/x", "x/", "x.", "a..b", "a//b", "a@{b", ".hidden/x", "x/.y", "seg.lock/x", "x;rm -rf", "a b", strings.Repeat("z", 201)}
	for _, b := range good {
		if !ValidBranchName(b) {
			t.Errorf("%q should be valid", b)
		}
	}
	for _, b := range bad {
		if ValidBranchName(b) {
			t.Errorf("%q should be invalid", b)
		}
	}
}

func TestStop_Respawn_Argv(t *testing.T) {
	f := newFake()
	a := New(f)
	s := &model.Session{Key: "sup-x", ID: "abc123"}
	if err := a.Stop(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if err := a.Respawn(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if f.argv(0) != "claude stop abc123" || f.argv(1) != "claude respawn abc123" {
		t.Errorf("argv: %q, %q", f.argv(0), f.argv(1))
	}
	// interactive session (no id) refused
	if err := a.Stop(context.Background(), &model.Session{Key: "k"}); err == nil {
		t.Error("stop without id should fail")
	}
}

func TestOpenInTmux_Argv(t *testing.T) {
	f := newFake()
	f.errs["tmux has-session"] = &collect.ExitError{Cmd: "tmux", Code: 1} // session absent
	f.outputs["tmux new-window"] = []byte("clauditor:3\n")
	a := New(f)
	res, err := a.OpenInTmux(context.Background(), &model.Session{Key: "k", ID: "abc123", Name: "kms rotation!", CWD: "/repos/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "clauditor:3" {
		t.Errorf("target = %q", res.Target)
	}
	joined := ""
	for _, c := range f.calls {
		joined += strings.Join(append([]string{c.name}, c.args...), " ") + "\n"
	}
	for _, want := range []string{
		"tmux has-session -t clauditor",
		"tmux new-session -d -s clauditor",
		"-c /repos/alpha",
		"claude attach abc123",
		"-n kms-rotation-", // sanitized window name
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestSlugForBranch(t *testing.T) {
	tests := map[string]string{
		"feat/kms-rotation": "feat-kms-rotation",
		"Feature/Big Thing": "feature-big-thing",
		"a":                 "a",
	}
	for in, want := range tests {
		if got := SlugForBranch(in); got != want {
			t.Errorf("SlugForBranch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsChoiceNumber(t *testing.T) {
	good := []string{"1", "9", "10", "42"}
	bad := []string{"", "a", "1a", "123", "-1", " 1"}
	for _, s := range good {
		if !isChoiceNumber(s) {
			t.Errorf("%q should be a valid choice", s)
		}
	}
	for _, s := range bad {
		if isChoiceNumber(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
}

func TestValidSessionID(t *testing.T) {
	if !ValidSessionID("c89e4641") || !ValidSessionID("f290fb7a-dbaf-4623-914d-87405b8c67a9") {
		t.Error("real ids should validate")
	}
	for _, bad := range []string{"", "x; rm -rf /", "id$(cmd)", "a b", "abc"} {
		if ValidSessionID(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// Reply and OpenInTmux must refuse shell-metacharacter ids before any exec.
func TestActions_RejectMalformedIDs(t *testing.T) {
	f := newFake()
	a := New(f)
	if err := a.Reply(context.Background(), "x;evil", "hello"); err == nil {
		t.Error("Reply accepted a malformed id")
	}
	if _, err := a.OpenInTmux(context.Background(), &model.Session{Key: "k", ID: "x;evil"}); err == nil {
		t.Error("OpenInTmux accepted a malformed id")
	}
	if len(f.calls) != 0 {
		t.Errorf("no exec should happen for malformed ids, got %d calls", len(f.calls))
	}
}
