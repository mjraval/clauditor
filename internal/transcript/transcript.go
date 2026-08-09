// Package transcript renders Claude Code session transcripts
// (`${CLAUDE_CONFIG_DIR:-~/.claude}/projects/*/<sessionId>.jsonl`) into clean,
// tail-first conversation lines for the cockpit preview pane.
//
// The cockpit uses this for pane-less background sessions: `claude logs` is a
// raw ANSI screen replay whose only layout information is the escape stream, so
// stripping it mashes words together (the v1 top complaint). A transcript, by
// contrast, is a structured JSONL log of the actual messages — parsing it and
// re-rendering `❯`/`●`/`⚒` lines gives readable output regardless of how the
// live terminal wrapped it.
//
// Parsing is deliberately tolerant, mirroring internal/collect's agents-json
// discipline: unknown element/block types are skipped, a corrupt line never
// drops the rest, and every field is optional.
package transcript

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DefaultTailBytes is how much of the tail of a transcript is read for a
// preview. Transcripts grow without bound; only the last conversation turns
// matter, and reading the whole file for every preview refresh would be waste.
const DefaultTailBytes int64 = 256 * 1024

// Kind classifies a rendered transcript entry.
type Kind int

const (
	KindUser      Kind = iota // a human prompt        → ❯
	KindAssistant             // an assistant text turn → ●
	KindTool                  // an assistant tool call → ⚒ (dim)
)

// Entry is one rendered line of a transcript: a role and its display text
// (already flattened — for a tool call, Text is "<tool> <hint>").
type Entry struct {
	Kind Kind
	Text string
}

// Glyph is the leading marker for the entry's kind.
func (e Entry) Glyph() string {
	switch e.Kind {
	case KindUser:
		return "❯"
	case KindTool:
		return "⚒"
	default:
		return "●"
	}
}

// Line is the full "<glyph> <text>" rendering.
func (e Entry) Line() string {
	if e.Text == "" {
		return e.Glyph()
	}
	return e.Glyph() + " " + e.Text
}

// wireMessage is the tolerant shape of one transcript element. Only the fields
// the preview renders are decoded; everything else (ai-title, mode, attachment,
// permission-mode, file-history-*, …) is ignored.
type wireMessage struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
	// IsSidechain marks Task-tool/subagent turns interleaved into the same
	// JSONL. The preview shows the MAIN thread; a busy subagent could
	// otherwise flood the whole visible tail with its chatter (QA finding).
	IsSidechain bool `json:"isSidechain"`
}

type wireInner struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type wireBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ParseLine renders one JSONL element into zero or more display entries. A
// single assistant message may carry several content blocks (text + tool_use),
// each becoming its own entry. Corrupt or non-conversational elements yield no
// entries (they are skipped, never fatal).
func ParseLine(raw []byte) []Entry {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	var msg wireMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil
	}
	if msg.Type != "user" && msg.Type != "assistant" {
		return nil // ai-title, mode, attachment, tool_result envelopes, …
	}
	if msg.IsSidechain {
		return nil // subagent thread — main-thread turns only
	}
	var inner wireInner
	if err := json.Unmarshal(msg.Message, &inner); err != nil {
		return nil
	}

	// content may be a plain string (some user turns) or an array of blocks.
	if s, ok := decodeString(inner.Content); ok {
		if e, ok := userTextEntry(s); ok {
			return []Entry{e}
		}
		return nil
	}
	var blocks []wireBlock
	if err := json.Unmarshal(inner.Content, &blocks); err != nil {
		return nil
	}
	var out []Entry
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if inner.Role == "user" {
				if e, ok := userTextEntry(b.Text); ok {
					out = append(out, e)
				}
				continue
			}
			if t := flatten(b.Text); t != "" {
				out = append(out, Entry{Kind: KindAssistant, Text: t})
			}
		case "tool_use":
			out = append(out, Entry{Kind: KindTool, Text: toolHint(b)})
			// thinking, tool_result, image, and unknown block types are skipped.
		}
	}
	return out
}

// userTextEntry renders a human prompt, dropping the synthetic system envelopes
// Claude Code injects as user turns (local-command caveats, command wrappers).
func userTextEntry(s string) (Entry, bool) {
	t := flatten(s)
	if t == "" || strings.HasPrefix(t, "<") {
		return Entry{}, false
	}
	return Entry{Kind: KindUser, Text: t}, true
}

// toolHint renders a tool_use block as "<Name> <hint>": the tool name plus the
// most identifying argument (command / file / pattern), truncated.
func toolHint(b wireBlock) string {
	name := b.Name
	if name == "" {
		name = "tool"
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(b.Input, &args) == nil {
		for _, k := range []string{"command", "file_path", "path", "pattern", "url", "prompt", "description"} {
			if v, ok := decodeString(args[k]); ok {
				if h := flatten(v); h != "" {
					return name + " " + truncate(h, 48)
				}
			}
		}
	}
	return name
}

// decodeString reports whether raw is a JSON string, returning its value.
func decodeString(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// flatten collapses whitespace/newlines into single spaces so a multi-line
// message renders as one preview line.
func flatten(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// Parse renders every element of a JSONL transcript blob, in file order.
// Lines are split manually rather than via bufio.Scanner: a Scanner with any
// buffer cap aborts the whole stream on one oversized line (QA finding —
// "one 9MB tool_result silently dropped every later turn"), while manual
// splitting skips only the unparseable element, honoring the package promise
// that a corrupt line never drops the rest.
func Parse(data []byte) []Entry {
	var out []Entry
	for len(data) > 0 {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			line, data = data, nil
		}
		out = append(out, ParseLine(line)...)
	}
	return out
}

// Resolve finds the transcript file for a session id under the projects tree,
// or ("", false) if none exists. The projects tree is per-project-directory,
// so the file may live under any project subdir.
func Resolve(sessionID string) (string, bool) {
	if sessionID == "" {
		return "", false
	}
	matches, err := filepath.Glob(filepath.Join(projectsDir(), "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

// projectsDir is ${CLAUDE_CONFIG_DIR:-~/.claude}/projects.
func projectsDir() string {
	base := os.Getenv("CLAUDE_CONFIG_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "projects" // best-effort; Glob simply finds nothing
		}
		base = filepath.Join(home, ".claude")
	}
	return filepath.Join(base, "projects")
}

// readTail returns the last maxBytes of the file, dropping the leading partial
// line when the read started mid-line (so Parse never sees a truncated head).
func readTail(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // path derived from a validated session id under the config tree
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	start := int64(0)
	if maxBytes > 0 && size > maxBytes {
		start = size - maxBytes
	}
	if start > 0 {
		if _, err := f.Seek(start, 0); err != nil {
			return nil, err
		}
	}
	data := make([]byte, size-start)
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, err
	}
	if start > 0 {
		// Drop the leading partial line ONLY when a complete line follows it —
		// when the whole window sits inside one line larger than maxBytes,
		// dropping would discard everything and the preview would lie with
		// "(no transcript yet)" (QA finding). Keeping the partial is safe:
		// the tolerant parser skips what it can't decode.
		if i := bytes.IndexByte(data, '\n'); i >= 0 && i+1 < len(data) {
			data = data[i+1:]
		}
	}
	return data, nil
}

// Render is the preview entry point: it resolves the session's transcript,
// reads its tail, and returns rendered conversation lines. A missing/unresolved
// transcript yields ["(no transcript yet)"]; a resolvable-but-empty one the
// same. Never returns an error the caller must handle — the preview always has
// something honest to show.
func Render(sessionID string, maxBytes int64) []string {
	path, ok := Resolve(sessionID)
	if !ok {
		return []string{"(no transcript yet)"}
	}
	return RenderFile(path, maxBytes)
}

// RenderFile renders a specific transcript file's tail (exposed for tests and
// callers that already resolved the path).
func RenderFile(path string, maxBytes int64) []string {
	data, err := readTail(path, maxBytes)
	if err != nil {
		return []string{"(no transcript yet)"}
	}
	entries := Parse(data)
	if len(entries) == 0 {
		// Distinguish "nothing happened yet" from "the recent entries are too
		// large/opaque to render" — the latter must not read as an idle session.
		if len(bytes.TrimSpace(data)) > 0 {
			return []string{"(recent transcript entries too large to preview — attach for detail)"}
		}
		return []string{"(no transcript yet)"}
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Line()
	}
	return out
}
