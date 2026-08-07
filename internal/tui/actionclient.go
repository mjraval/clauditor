package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// humanReplyErr turns a reply failure into a keyboard-friendly status message.
// Local failures arrive as *actions.ActionError (mapped by code); daemon
// failures (including the 501 when experimental_reply is off) are plain errors
// and are surfaced verbatim.
func humanReplyErr(err error) string {
	var ae *actions.ActionError
	if errors.As(err, &ae) {
		switch ae.Code {
		case "permission_prompt":
			return "permission prompt — attach to answer it (enter)"
		default:
			return ae.Message
		}
	}
	return err.Error()
}

// ActionClient performs mutating operations (open-in-tmux, stop, dispatch),
// either through the daemon HTTP API or directly via internal/actions
// (SPEC §11: "Actions go through the daemon when connected, else direct").
type ActionClient interface {
	Stop(ctx context.Context, sess *model.Session) error
	Respawn(ctx context.Context, sess *model.Session) error
	OpenInTmux(ctx context.Context, sess *model.Session) (*actions.OpenInTmuxResult, error)
	Dispatch(ctx context.Context, snap *model.Snapshot, req actions.DispatchRequest) (*actions.DispatchResult, error)
	// Reply delivers text to a session waiting on input. NOTE: the local
	// implementation is deliberately NOT gated behind
	// actions.experimental_reply — a user at the physical keyboard has the
	// same trust as one who would attach and type, so the cockpit lets them
	// reply directly. The daemon path stays gated and may return 501.
	Reply(ctx context.Context, sess *model.Session, text string) error
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

func (d *daemonActionClient) Respawn(ctx context.Context, sess *model.Session) error {
	return d.postJSON(ctx, "/api/v1/sessions/"+url.PathEscape(sess.Key)+"/respawn", nil, nil)
}

// Reply POSTs to the daemon's reply endpoint. The daemon may answer 501 when
// actions.experimental_reply is off; that message is surfaced verbatim.
func (d *daemonActionClient) Reply(ctx context.Context, sess *model.Session, text string) error {
	return d.postJSON(ctx, "/api/v1/sessions/"+url.PathEscape(sess.Key)+"/reply",
		map[string]string{"text": text}, nil)
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

func (l *localActionClient) Respawn(ctx context.Context, sess *model.Session) error {
	return l.a.Respawn(ctx, sess)
}

// Reply delivers text via the tmux-injection strategy (docs/REPLY.md). This
// path is intentionally ungated by actions.experimental_reply: a local user
// at the keyboard has the same trust as attaching and typing themselves.
func (l *localActionClient) Reply(ctx context.Context, sess *model.Session, text string) error {
	if sess.ID == "" {
		return fmt.Errorf("session has no background id to reply to")
	}
	return l.a.Reply(ctx, sess.ID, text)
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
