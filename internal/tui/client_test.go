package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mjraval/clauditor/internal/config"
	"github.com/mjraval/clauditor/internal/model"
)

func TestDaemonBaseURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8790": "http://127.0.0.1:8790",
		"0.0.0.0:8790":   "http://127.0.0.1:8790",
		":8790":          "http://127.0.0.1:8790",
		"":                "http://127.0.0.1:8790",
	}
	for in, want := range cases {
		if got := daemonBaseURL(in); got != want {
			t.Errorf("daemonBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// withStateDir points XDG_STATE_HOME at a temp dir for the duration of the
// test, so readLocalToken/config.StateDir don't touch the real home dir.
func withStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	sd, err := config.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	return sd
}

func TestReadLocalToken_Missing(t *testing.T) {
	withStateDir(t)
	tok, err := readLocalToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok != "" {
		t.Errorf("token = %q, want empty when file absent", tok)
	}
}

func TestReadLocalToken_Present(t *testing.T) {
	sd := withStateDir(t)
	if err := os.WriteFile(filepath.Join(sd, "local_token"), []byte("secret-tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := readLocalToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok != "secret-tok" {
		t.Errorf("token = %q, want %q", tok, "secret-tok")
	}
}

func TestDetectSource_FallsBackWithoutToken(t *testing.T) {
	withStateDir(t)
	cfg := config.Default()
	src, err := DetectSource(context.Background(), cfg, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if src.Label() != "in-process" {
		t.Errorf("Label() = %q, want in-process", src.Label())
	}
}

func TestDetectSource_UsesDaemonWhenReachable(t *testing.T) {
	sd := withStateDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(model.Snapshot{Version: 42})
	}))
	defer srv.Close()

	if err := os.WriteFile(filepath.Join(sd, "local_token"), []byte("good-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Serve.Listen = srv.Listener.Addr().String()

	src, err := DetectSource(context.Background(), cfg, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if src.Label() != "daemon" {
		t.Fatalf("Label() = %q, want daemon", src.Label())
	}
	snap, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Version != 42 {
		t.Errorf("Version = %d, want 42", snap.Version)
	}
}

func TestDetectSource_FallsBackOnBadToken(t *testing.T) {
	sd := withStateDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_token","message":"nope"}}`))
	}))
	defer srv.Close()

	if err := os.WriteFile(filepath.Join(sd, "local_token"), []byte("stale-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Serve.Listen = srv.Listener.Addr().String()

	src, err := DetectSource(context.Background(), cfg, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if src.Label() != "in-process" {
		t.Errorf("Label() = %q, want in-process (bad token should fall back)", src.Label())
	}
}

func TestDaemonSource_FetchLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sessions/sup-abc/logs" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("lines") != "500" {
			t.Errorf("unexpected lines query %q", r.URL.Query().Get("lines"))
		}
		_, _ = w.Write([]byte("log line 1\nlog line 2\n"))
	}))
	defer srv.Close()

	d := &daemonSource{baseURL: srv.URL, token: "t", client: srv.Client()}
	text, err := d.FetchLogs(context.Background(), &model.Session{Key: "sup-abc"}, 500)
	if err != nil {
		t.Fatal(err)
	}
	if text != "log line 1\nlog line 2\n" {
		t.Errorf("text = %q", text)
	}
}
