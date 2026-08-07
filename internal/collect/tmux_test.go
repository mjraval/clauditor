package collect

import "testing"

const panesFixture = "core-app\t1\t1\t%5\t93174\tclaude\t/home/u/projects/mono/core-app\tclaude\t1\n" +
	"core-infra\t1\t1\t%7\t93819\tbash\t/home/u/projects/mono/core-infra\tshell\t0\n" +
	"short-line\t1\n" +
	"weird\tx\ty\t%9\tz\tclaude\t/tmp\tw\t1\n"

func TestParsePanes(t *testing.T) {
	panes := ParsePanes([]byte(panesFixture))
	if len(panes) != 3 { // short line dropped; weird line kept with zeroed numerics
		t.Fatalf("got %d panes, want 3", len(panes))
	}
	p := panes[0]
	if p.SessionName != "core-app" || p.PaneID != "%5" || p.PanePID != 93174 ||
		p.CurrentCommand != "claude" || !p.SessionAttached {
		t.Errorf("pane 0 parsed wrong: %+v", p)
	}
	if p.Target() != "core-app:1.1" {
		t.Errorf("target = %q", p.Target())
	}
	if panes[1].SessionAttached {
		t.Error("pane 1 should be detached")
	}
	if panes[2].PanePID != 0 {
		t.Errorf("weird line pid should degrade to 0, got %d", panes[2].PanePID)
	}
}

const psFixture = `    1       0 /sbin/init
  100       1 tmux
  200     100 bash
  300     200 node /usr/local/lib/claude
  400     200 vim main.go
  500     100 claude
  600       1 /home/u/.local/bin/claude-tmux
  700     100 zsh
  800     700 /usr/bin/claude --resume abc
`

func TestParsePS_And_PIDTree(t *testing.T) {
	procs := ParsePS([]byte(psFixture))
	if len(procs) != 9 {
		t.Fatalf("got %d procs, want 9", len(procs))
	}
	tree := NewPIDTree(procs)

	// bash(200) under tmux pane: its claude is the node wrapper (300)
	if pid, ok := tree.FindClaudeDescendant(200); !ok || pid != 300 {
		t.Errorf("FindClaudeDescendant(200) = %d,%v want 300,true", pid, ok)
	}
	// pane whose pid IS claude directly
	if pid, ok := tree.FindClaudeDescendant(500); !ok || pid != 500 {
		t.Errorf("FindClaudeDescendant(500) = %d,%v want 500,true", pid, ok)
	}
	// zsh(700) → /usr/bin/claude with args
	if pid, ok := tree.FindClaudeDescendant(700); !ok || pid != 800 {
		t.Errorf("FindClaudeDescendant(700) = %d,%v want 800,true", pid, ok)
	}
	// claude-tmux must NOT match (substring trap from prior art)
	if pid, ok := tree.FindClaudeDescendant(600); ok {
		t.Errorf("claude-tmux false positive: pid %d", pid)
	}
	// vim-only subtree
	if _, ok := tree.FindClaudeDescendant(400); ok {
		t.Error("vim subtree should have no claude")
	}

	if !tree.ContainsPID(100, 300) {
		t.Error("ContainsPID(100,300) should be true (tmux→bash→node)")
	}
	if tree.ContainsPID(200, 500) {
		t.Error("ContainsPID(200,500) should be false (sibling subtree)")
	}
}

func TestIsClaudeCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"claude", true},
		{"/usr/bin/claude --resume x", true},
		{"node /usr/local/lib/node_modules/@anthropic-ai/claude-code/cli.js", false}, // script basename is cli.js, not claude — pid walk relies on argv0 or script named claude
		{"node /home/u/.nvm/bin/claude", true},
		{"bun /home/u/.bun/bin/claude", true},
		{"claude-tmux", false},
		{"clauditor serve", false},
		{"claude-monitor --watch", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isClaudeCommand(tt.cmd); got != tt.want {
			t.Errorf("isClaudeCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}
