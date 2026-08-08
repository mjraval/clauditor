package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mjraval/clauditor/internal/actions"
	"github.com/mjraval/clauditor/internal/model"
)

func TestDaemonActionClient_StopSendsRequiredHeaders(t *testing.T) {
	var gotPath, gotAuth, gotAction, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAction = r.Header.Get("X-Clauditor-Action")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
	}))
	defer srv.Close()

	ac := &daemonActionClient{baseURL: srv.URL, token: "tok", client: srv.Client()}
	if err := ac.Stop(context.Background(), &model.Session{Key: "sup-x"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/sessions/sup-x/stop" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotAction != "1" {
		t.Errorf("X-Clauditor-Action = %q, want 1", gotAction)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
}

func TestDaemonActionClient_StopMapsErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"missing_action_header","message":"nope"}}`))
	}))
	defer srv.Close()

	ac := &daemonActionClient{baseURL: srv.URL, token: "tok", client: srv.Client()}
	err := ac.Stop(context.Background(), &model.Session{Key: "sup-x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing_action_header") {
		t.Errorf("error should surface the daemon's code: %v", err)
	}
}

func TestDaemonActionClient_OpenInTmux(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/open-in-tmux") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(actions.OpenInTmuxResult{Target: "clauditor:3", Attach: "tmux attach -t clauditor"})
	}))
	defer srv.Close()

	ac := &daemonActionClient{baseURL: srv.URL, token: "tok", client: srv.Client()}
	res, err := ac.OpenInTmux(context.Background(), &model.Session{Key: "sup-x"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "clauditor:3" {
		t.Errorf("target = %q", res.Target)
	}
}

func TestDaemonActionClient_DispatchSendsRequestBody(t *testing.T) {
	var gotReq actions.DispatchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/dispatch" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(actions.DispatchResult{Dir: "/repos/alpha", ShortID: "abc123"})
	}))
	defer srv.Close()

	ac := &daemonActionClient{baseURL: srv.URL, token: "tok", client: srv.Client()}
	req := actions.DispatchRequest{Prompt: "do it", Target: actions.DispatchTarget{Repo: "alpha", Worktree: "/repos/alpha"}}
	res, err := ac.Dispatch(context.Background(), nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.ShortID != "abc123" {
		t.Errorf("shortId = %q", res.ShortID)
	}
	if gotReq.Prompt != "do it" || gotReq.Target.Repo != "alpha" {
		t.Errorf("request body not forwarded: %+v", gotReq)
	}
}
