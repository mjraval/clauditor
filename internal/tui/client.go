package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rishi/clauditor/internal/collect"
	"github.com/rishi/clauditor/internal/config"
	"github.com/rishi/clauditor/internal/model"
	"github.com/rishi/clauditor/internal/store"
)

// Source fetches fleet snapshots. Two implementations: daemonSource talks to
// a running `clauditor serve` over loopback HTTP; localSource runs the
// collectors in-process (SPEC §11 fallback).
type Source interface {
	Fetch(ctx context.Context) (*model.Snapshot, error)
	Label() string // "daemon" | "in-process"
}

// --- daemon source -------------------------------------------------------

type daemonSource struct {
	baseURL string
	token   string
	client  *http.Client
}

func (d *daemonSource) Label() string { return "daemon" }

func (d *daemonSource) Fetch(ctx context.Context) (*model.Snapshot, error) {
	var snap model.Snapshot
	if err := d.getJSON(ctx, "/api/v1/state", &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func (d *daemonSource) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return daemonError(resp.StatusCode, body)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

// errorEnvelope mirrors internal/api/server.go's {error:{code,message}}.
type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func daemonError(status int, body []byte) error {
	var e errorEnvelope
	if json.Unmarshal(body, &e) == nil && e.Error.Code != "" {
		return fmt.Errorf("daemon: %s: %s", e.Error.Code, e.Error.Message)
	}
	return fmt.Errorf("daemon: http %d: %s", status, strings.TrimSpace(string(body)))
}

// LogFetcher is implemented by both sources so the `l` logs-peek overlay
// doesn't need to care which one is active.
type LogFetcher interface {
	FetchLogs(ctx context.Context, sess *model.Session, lines int) (string, error)
}

// PreviewFetcher is implemented by both sources so the live-preview pane
// doesn't need to know which one is active. The daemon source reads the
// authenticated GET /logs endpoint; the in-process source shells out to the
// collectors directly (pane capture, else `claude logs`).
type PreviewFetcher interface {
	FetchPreview(ctx context.Context, sess *model.Session, lines int) (string, error)
}

// FetchPreview reads the daemon's logs endpoint (already ANSI-stripped
// server-side) for the preview pane.
func (d *daemonSource) FetchPreview(ctx context.Context, sess *model.Session, lines int) (string, error) {
	return d.FetchLogs(ctx, sess, lines)
}

// FetchLogs fetches a session's log/pane-capture text over HTTP.
func (d *daemonSource) FetchLogs(ctx context.Context, sess *model.Session, lines int) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/v1/sessions/%s/logs?lines=%d", d.baseURL, url.PathEscape(sess.Key), lines), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", daemonError(resp.StatusCode, body)
	}
	return string(body), nil
}

// --- in-process (fallback) source ----------------------------------------

type localSource struct {
	poller *store.Poller
}

func (l *localSource) Label() string { return "in-process" }

func (l *localSource) Fetch(ctx context.Context) (*model.Snapshot, error) {
	return l.poller.RunOnce(ctx), nil
}

// FetchLogs is the in-process fallback: capture straight from the
// collectors (mirrors internal/api/handlers.go's handleLogs source
// selection).
func (l *localSource) FetchLogs(ctx context.Context, sess *model.Session, lines int) (string, error) {
	switch {
	case sess.ID != "":
		out, err := l.poller.Fleet.Claude.Logs(ctx, sess.ID, 256*1024)
		return string(out), err
	case sess.TmuxPaneID != "":
		out, err := l.poller.Fleet.Tmux.CapturePane(ctx, sess.TmuxPaneID, lines, false)
		return string(out), err
	default:
		return "", fmt.Errorf("session has neither a background id nor a tmux pane")
	}
}

// FetchPreview is the in-process preview source: a live tmux pane wins (the
// actual terminal a human sees), otherwise `claude logs` ANSI-stripped. The
// selection order is previewSourceKind's contract (pane-first), the opposite
// of FetchLogs (which is ID-first, matching the daemon /logs endpoint).
func (l *localSource) FetchPreview(ctx context.Context, sess *model.Session, lines int) (string, error) {
	switch previewSourceKind(sess) {
	case previewPane:
		out, err := l.poller.Fleet.Tmux.CapturePane(ctx, sess.TmuxPaneID, lines, false)
		return string(out), err
	case previewLogs:
		out, err := l.poller.Fleet.Claude.Logs(ctx, sess.ID, 256*1024)
		return string(collect.StripANSI(out)), err
	default:
		return "", fmt.Errorf("session has neither a tmux pane nor a background id")
	}
}

// --- setup -----------------------------------------------------------------

// newFleet mirrors cmd/clauditor/main.go's newFleet — duplicated here
// because internal/tui cannot import package main.
func newFleet(cfg *config.Config, includeAll bool) *collect.Fleet {
	r := collect.NewRunner()
	git := collect.NewGitCollector(r)
	git.DirtyCheck = cfg.Git.DirtyCheck
	git.AheadBehind = cfg.Git.AheadBehind
	return &collect.Fleet{
		Claude:        collect.NewClaudeCollector(r),
		Tmux:          collect.NewTmuxCollector(r),
		Git:           git,
		Repos:         cfg.Repos,
		WorkspaceDirs: cfg.WorkspaceDirs,
		IncludeAll:    includeAll,
	}
}

// newLocalSource builds the in-process fallback poller.
func newLocalSource(cfg *config.Config) *localSource {
	return &localSource{poller: &store.Poller{Fleet: newFleet(cfg, true), Store: store.New(), Cfg: cfg}}
}

// readLocalToken reads <state-dir>/local_token (SPEC §11). Returns "" (no
// error) when the file doesn't exist — that's the normal "daemon not
// running" case, not a failure.
func readLocalToken() (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "local_token"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// daemonBaseURL turns [serve].listen into an http:// base URL. An empty
// host (e.g. "0.0.0.0:8790" or ":8790") is rewritten to loopback since the
// TUI only ever talks to a same-box daemon.
func daemonBaseURL(listen string) string {
	if listen == "" {
		listen = "127.0.0.1:8790"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://" + listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + host + ":" + port
}

// DetectSource tries the local daemon first (SPEC §11: local_token +
// GET /api/v1/state), falling back to the in-process collectors when the
// token file is missing or the daemon doesn't answer. probeTimeout bounds
// the daemon probe so a dead/hung daemon doesn't stall startup.
func DetectSource(ctx context.Context, cfg *config.Config, probeTimeout time.Duration) (Source, error) {
	token, err := readLocalToken()
	if err != nil {
		return nil, fmt.Errorf("read local_token: %w", err)
	}
	if token != "" {
		d := &daemonSource{
			baseURL: daemonBaseURL(cfg.Serve.Listen),
			token:   token,
			client:  &http.Client{Timeout: probeTimeout},
		}
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		defer cancel()
		if _, err := d.Fetch(probeCtx); err == nil {
			return d, nil
		}
		// Daemon unreachable (or not yet warmed up) — fall back below.
	}
	return newLocalSource(cfg), nil
}
