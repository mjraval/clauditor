// Package api serves clauditor's HTTP surface: read routes, SSE, healthz,
// and the gated action endpoints (SPEC §7.2, §9).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/rishi/clauditor/internal/actions"
	"github.com/rishi/clauditor/internal/collect"
	"github.com/rishi/clauditor/internal/config"
	"github.com/rishi/clauditor/internal/store"
	"github.com/rishi/clauditor/internal/version"
)

// Server wires everything the handlers need.
type Server struct {
	Store   *store.Store
	Cfg     *config.Config
	Auth    *AuthConfig
	Actions *actions.Actions
	Claude  *collect.ClaudeCollector
	Tmux    *collect.TmuxCollector
	WebFS   fs.FS // embedded UI; nil = no UI
}

// errorEnvelope is the consistent error shape {error:{code,message}}.
type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	var e errorEnvelope
	e.Error.Code = code
	e.Error.Message = msg
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(e)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// mapActionErr translates ActionError codes to HTTP statuses.
func mapActionErr(w http.ResponseWriter, err error) {
	var ae *actions.ActionError
	if !errors.As(err, &ae) {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	status := http.StatusInternalServerError
	switch ae.Code {
	case "bad_target", "bad_request", "bad_branch", "bad_base":
		status = http.StatusBadRequest
	case "forbidden_flag":
		status = http.StatusForbidden
	case "permission_prompt":
		status = http.StatusConflict
	case "timeout", "delivery_unverified":
		status = http.StatusBadGateway
	}
	writeErr(w, status, ae.Code, ae.Message)
}

// Handler builds the full route tree.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// healthz is unauthenticated (SPEC §7.2) and deliberately terse.
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	authed := func(h http.HandlerFunc) http.Handler { return s.Auth.Auth(h) }
	guard := NewMutatingGuard(s.Cfg.Actions.Enabled, 10)

	mux.Handle("GET /api/v1/state", authed(s.handleState))
	mux.Handle("GET /api/v1/sessions/{key}", authed(s.handleSession))
	mux.Handle("GET /api/v1/sessions/{key}/logs", authed(s.handleLogs))
	mux.Handle("GET /api/v1/events", authed(s.handleEvents))

	mux.Handle("POST /api/v1/dispatch", s.Auth.Auth(guard.Wrap("dispatch", s.handleDispatch)))
	mux.Handle("POST /api/v1/sessions/{key}/stop", s.Auth.Auth(guard.Wrap("stop", s.handleStop)))
	mux.Handle("POST /api/v1/sessions/{key}/respawn", s.Auth.Auth(guard.Wrap("respawn", s.handleRespawn)))
	mux.Handle("POST /api/v1/sessions/{key}/open-in-tmux", s.Auth.Auth(guard.Wrap("open-in-tmux", s.handleOpenInTmux)))
	mux.Handle("POST /api/v1/sessions/{key}/reply", s.Auth.Auth(guard.Wrap("reply", s.handleReply)))

	if s.WebFS != nil {
		mux.Handle("GET /", s.Auth.Auth(http.FileServerFS(s.WebFS)))
	}
	return mux
}

// ListenAndServe binds (loopback-only unless explicitly overridden) and serves.
func (s *Server) ListenAndServe(ctx context.Context, listen string, allowExposed bool) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("bad listen address %q: %w", listen, err)
	}
	ip := net.ParseIP(host)
	if (ip == nil || !ip.IsLoopback()) && !allowExposed {
		return fmt.Errorf("refusing to bind non-loopback address %q — cloudflared fronts clauditor; use --i-know-this-is-exposed to override", listen)
	}

	srv := &http.Server{
		Addr:              listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"ok":         true,
		"version":    version.Version,
		"collectors": s.Store.CollectorAges(time.Now()),
	})
}
