package transcript

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []Entry
	}{
		{"user text block", `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Build it."}]}}`,
			[]Entry{{KindUser, "Build it."}}},
		{"user string content", `{"type":"user","message":{"role":"user","content":"just do it"}}`,
			[]Entry{{KindUser, "just do it"}}},
		{"user caveat envelope dropped", `{"type":"user","message":{"role":"user","content":"<local-command-caveat>x</local-command-caveat>"}}`,
			nil},
		{"assistant text", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello\nthere"}]}}`,
			[]Entry{{KindAssistant, "Hello there"}}},
		{"assistant thinking skipped", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"}]}}`,
			nil},
		{"tool_use with command hint", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`,
			[]Entry{{KindTool, "Bash go test ./..."}}},
		{"tool_use with file hint", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/a/b.go"}}]}}`,
			[]Entry{{KindTool, "Edit /a/b.go"}}},
		{"tool_use no input", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Foo"}]}}`,
			[]Entry{{KindTool, "Foo"}}},
		{"tool_result envelope skipped", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
			nil},
		{"multi-block assistant", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done."},{"type":"tool_use","name":"Read","input":{"file_path":"x"}}]}}`,
			[]Entry{{KindAssistant, "Done."}, {KindTool, "Read x"}}},
		{"unknown top type skipped", `{"type":"ai-title","title":"t"}`, nil},
		{"corrupt json skipped", `{not json`, nil},
		{"empty line skipped", ``, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseLine([]byte(c.raw))
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseLine = %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestRenderFile_Fixture(t *testing.T) {
	lines := RenderFile(filepath.Join("testdata", "sample.jsonl"), DefaultTailBytes)
	want := []string{
		"❯ Build the cockpit per the spec.",
		"● I'll start by reading the existing layout.",
		"⚒ Bash go build ./...",
		"⚒ Edit /repo/internal/tui/app.go",
		"● Done. The build is green.",
		"⚒ Read README.md",
		"❯ Continue from where you left off.",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("RenderFile fixture:\n got %#v\nwant %#v", lines, want)
	}
}

func TestRenderFile_Missing(t *testing.T) {
	lines := RenderFile(filepath.Join("testdata", "does-not-exist.jsonl"), DefaultTailBytes)
	if len(lines) != 1 || lines[0] != "(no transcript yet)" {
		t.Errorf("missing file = %v, want [(no transcript yet)]", lines)
	}
}

func TestRender_Unresolved(t *testing.T) {
	if lines := Render("", DefaultTailBytes); len(lines) != 1 || lines[0] != "(no transcript yet)" {
		t.Errorf("empty id = %v, want [(no transcript yet)]", lines)
	}
}

func TestReadTail_DropsPartialHead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	// Three full lines; a small tail budget forces a mid-line start.
	body := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"AAentryone"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"BBentrytwo"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"CCentrythree"}]}}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	lines := RenderFile(path, 160) // enough for ~1.5 lines
	// The leading partial line must have been dropped, so no mangled entry.
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "AAentryone") {
		t.Errorf("partial head line should be dropped, got %q", joined)
	}
	if !strings.Contains(joined, "CCentrythree") {
		t.Errorf("tail should retain the last full line, got %q", joined)
	}
}

func TestResolve_UsesConfigDir(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "projects", "-some-repo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "abc-123"
	if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	got, ok := Resolve(id)
	if !ok || filepath.Base(got) != id+".jsonl" {
		t.Errorf("Resolve = %q,%v; want the fixture path", got, ok)
	}
	if _, ok := Resolve("nope"); ok {
		t.Errorf("Resolve of a missing id should be false")
	}
}
