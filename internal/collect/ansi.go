package collect

import "regexp"

// ansiRe strips CSI/OSC escape sequences and non-printing control characters
// from `claude logs` raw ANSI screen replays (and, when requested, tmux
// captures). Shared by the HTTP API (internal/api) and the TUI preview pane so
// there is exactly one definition of "what a replay looks like once cleaned".
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][0-9A-B]|[\x00-\x08\x0b\x0c\x0e-\x1f]`)

// StripANSI removes ANSI escape sequences and non-printing control characters
// from b, returning a fresh slice.
func StripANSI(b []byte) []byte {
	return ansiRe.ReplaceAll(b, nil)
}
