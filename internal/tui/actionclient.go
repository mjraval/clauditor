package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"

	"github.com/rishi/clauditor/internal/actions"
	"github.com/rishi/clauditor/internal/collect"
	"github.com/rishi/clauditor/internal/config"
	"github.com/rishi/clauditor/internal/model"
)

// ActionClient performs mutating operations (open-in-tmux, stop, dispatch),
// either through the daemon HTTP API or directly via internal/actions
// (SPEC §11: "Actions go through the daemon when connected, else direct").
type ActionClient interface {
	Stop(ctx context.Context, sess *model.Session) error
	OpenInTmux(ctx context.Context, sess *model.Session) (*actions.OpenInTmuxResult, error)
	Dispatch(ctx context.Context, snap *model.Snapshot, req actions.DispatchRequest) (*actions.DispatchResult, error)
}

// --- daemon-backed action client -----------------------------------------

type daemonActionClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newDaemonActionClient(d *daemonSource) *daemonActionClient {
	return &daemonActionClient{baseURL: d.baseURL, token: d.token, client: d.client}
}

func (d *daemonActionClient) postJSON(ctx context.Context, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set("X-Clauditor-Action", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return daemonError(resp.StatusCode, respBody)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func (d *daemonActionClient) Stop(ctx context.Context, sess *model.Session) error {
	return d.postJSON(ctx, "/api/v1/sessions/"+url.PathEscape(sess.Key)+"/stop", nil, nil)
}

func (d *daemonActionClient) OpenInTmux(ctx context.Context, sess *model.Session) (*actions.OpenInTmuxResult, error) {
	var res actions.OpenInTmuxResult
	if err := d.postJSON(ctx, "/api/v1/sessions/"+url.PathEscape(sess.Key)+"/open-in-tmux", nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (d *daemonActionClient) Dispatch(ctx context.Context, _ *model.Snapshot, req actions.DispatchRequest) (*actions.DispatchResult, error) {
	var res actions.DispatchResult
	if err := d.postJSON(ctx, "/api/v1/dispatch", req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// --- direct (in-process) action client -----------------------------------

type localActionClient struct {
	a *actions.Actions
}

func newLocalActionClient(cfg *config.Config) *localActionClient {
	a := actions.New(collect.NewRunner())
	a.WorktreeBase = cfg.Dispatch.WorktreeBase
	return &localActionClient{a: a}
}

func (l *localActionClient) Stop(ctx context.Context, sess *model.Session) error {
	return l.a.Stop(ctx, sess)
}

func (l *localActionClient) OpenInTmux(ctx context.Context, sess *model.Session) (*actions.OpenInTmuxResult, error) {
	return l.a.OpenInTmux(ctx, sess)
}

func (l *localActionClient) Dispatch(ctx context.Context, snap *model.Snapshot, req actions.DispatchRequest) (*actions.DispatchResult, error) {
	return l.a.Dispatch(ctx, snap, req)
}

// SwitchTmuxClient runs `tmux switch-client -t <target>` when the TUI itself
// is running inside tmux (SPEC §11: "if running inside tmux, also switch to
// it"). Best-effort: failures are returned for the caller to surface but
// never fatal to the TUI.
func SwitchTmuxClient(ctx context.Context, target string) error {
	if target == "" {
		return fmt.Errorf("no tmux target to switch to")
	}
	cmd := exec.CommandContext(ctx, "tmux", "switch-client", "-t", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux switch-client: %v: %s", err, string(out))
	}
	return nil
}
