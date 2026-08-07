package collect

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// Pane is one tmux pane from `list-panes -a`.
type Pane struct {
	SessionName     string
	WindowIndex     int
	PaneIndex       int
	PaneID          string // "%42"
	PanePID         int
	CurrentCommand  string
	CurrentPath     string
	WindowName      string
	SessionAttached bool
}

// Target renders the "session:window.pane" form.
func (p Pane) Target() string {
	return p.SessionName + ":" + strconv.Itoa(p.WindowIndex) + "." + strconv.Itoa(p.PaneIndex)
}

// paneFormat matches SPEC §5.2 field-for-field.
const paneFormat = "#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_id}\t#{pane_pid}\t#{pane_current_command}\t#{pane_current_path}\t#{window_name}\t#{session_attached}"

// ParsePanes decodes `tmux list-panes -a -F <paneFormat>` output. Short or
// malformed lines are skipped, not fatal.
func ParsePanes(data []byte) []Pane {
	var out []Pane
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\n")
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 9 {
			continue
		}
		wi, _ := strconv.Atoi(f[1])
		pi, _ := strconv.Atoi(f[2])
		pid, _ := strconv.Atoi(f[4])
		out = append(out, Pane{
			SessionName: f[0], WindowIndex: wi, PaneIndex: pi,
			PaneID: f[3], PanePID: pid,
			CurrentCommand: f[5], CurrentPath: f[6], WindowName: f[7],
			SessionAttached: f[8] != "0" && f[8] != "",
		})
	}
	return out
}

// Proc is one row of `ps -e -o pid=,ppid=,command=`.
type Proc struct {
	PID     int
	PPID    int
	Command string
}

// ParsePS decodes ps output (parsed once per scan cycle).
func ParsePS(data []byte) []Proc {
	var out []Proc
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(strings.TrimSpace(strings.TrimRight(line, "\n")))
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, Proc{PID: pid, PPID: ppid, Command: strings.Join(fields[2:], " ")})
	}
	return out
}

// PIDTree indexes processes by parent for subtree walks.
type PIDTree struct {
	children map[int][]Proc
	byPID    map[int]Proc
}

func NewPIDTree(procs []Proc) *PIDTree {
	t := &PIDTree{children: map[int][]Proc{}, byPID: map[int]Proc{}}
	for _, p := range procs {
		t.children[p.PPID] = append(t.children[p.PPID], p)
		t.byPID[p.PID] = p
	}
	return t
}

// FindClaudeDescendant walks the subtree under rootPID and returns the first
// process whose command's argv[0] basename is "claude", or a node/bun
// invocation of a claude script. Returns (pid, true) when found.
func (t *PIDTree) FindClaudeDescendant(rootPID int) (int, bool) {
	stack := []int{rootPID}
	seen := map[int]bool{}
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if p, ok := t.byPID[pid]; ok && pid != rootPID && isClaudeCommand(p.Command) {
			return pid, true
		}
		for _, c := range t.children[pid] {
			stack = append(stack, c.PID)
		}
	}
	// The pane pid itself may be claude (pane running claude directly).
	if p, ok := t.byPID[rootPID]; ok && isClaudeCommand(p.Command) {
		return rootPID, true
	}
	return 0, false
}

// ContainsPID reports whether needle is rootPID or a descendant of it.
func (t *PIDTree) ContainsPID(rootPID, needle int) bool {
	if rootPID == needle {
		return true
	}
	stack := []int{rootPID}
	seen := map[int]bool{}
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		for _, c := range t.children[pid] {
			if c.PID == needle {
				return true
			}
			stack = append(stack, c.PID)
		}
	}
	return false
}

// isClaudeCommand decides whether a ps command line is a Claude Code
// process. Exact-token match on the executable basename — never substring
// (which would false-positive on claude-tmux, clauditor itself, etc.).
func isClaudeCommand(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	base := fields[0]
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	if base == "claude" {
		return true
	}
	// npm-style installs: `node …/claude` or `bun …/claude`
	if (base == "node" || base == "bun") && len(fields) > 1 {
		script := fields[1]
		if i := strings.LastIndexByte(script, '/'); i >= 0 {
			script = script[i+1:]
		}
		return script == "claude"
	}
	return false
}

// TmuxCollector shells out to tmux and ps.
type TmuxCollector struct {
	Runner Runner
}

func NewTmuxCollector(r Runner) *TmuxCollector { return &TmuxCollector{Runner: r} }

// Panes lists every pane. A missing tmux server yields an empty set, never
// an error (SPEC §5.2).
func (c *TmuxCollector) Panes(ctx context.Context) ([]Pane, error) {
	out, err := c.Runner.Run(ctx, "", "tmux", "list-panes", "-a", "-F", paneFormat)
	if err != nil {
		if isNoServer(err) {
			return nil, nil
		}
		return nil, err
	}
	return ParsePanes(out), nil
}

// Processes snapshots the process table once per cycle.
func (c *TmuxCollector) Processes(ctx context.Context) ([]Proc, error) {
	out, err := c.Runner.Run(ctx, "", "ps", "-e", "-o", "pid=,ppid=,command=")
	if err != nil {
		return nil, err
	}
	return ParsePS(out), nil
}

// CapturePane grabs the last `lines` of a pane. keepANSI adds -e.
func (c *TmuxCollector) CapturePane(ctx context.Context, paneID string, lines int, keepANSI bool) ([]byte, error) {
	args := []string{"capture-pane", "-p", "-t", paneID, "-S", "-" + strconv.Itoa(lines)}
	if keepANSI {
		args = append(args, "-e")
	}
	return c.Runner.Run(ctx, "", "tmux", args...)
}

func isNoServer(err error) bool {
	var ee *ExitError
	if !errors.As(err, &ee) {
		return false
	}
	s := ee.Stderr
	return strings.Contains(s, "no server running") ||
		strings.Contains(s, "No such file or directory") ||
		strings.Contains(s, "no sessions")
}
