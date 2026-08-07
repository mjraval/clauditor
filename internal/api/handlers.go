package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/rishi/clauditor/internal/actions"
	"github.com/rishi/clauditor/internal/model"
)

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	snap := s.Store.Get()
	if snap == nil {
		writeErr(w, http.StatusServiceUnavailable, "no_snapshot", "collectors have not produced a snapshot yet")
		return
	}
	writeJSON(w, snap)
}

func (s *Server) sessionFromPath(w http.ResponseWriter, r *http.Request) *model.Session {
	snap := s.Store.Get()
	if snap == nil {
		writeErr(w, http.StatusServiceUnavailable, "no_snapshot", "collectors have not produced a snapshot yet")
		return nil
	}
	sess := snap.SessionByKey(r.PathValue("key"))
	if sess == nil {
		writeErr(w, http.StatusNotFound, "unknown_session", "no session with key "+r.PathValue("key"))
		return nil
	}
	return sess
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionFromPath(w, r); sess != nil {
		writeJSON(w, sess)
	}
}

// ansiRe strips CSI/OSC escape sequences from `claude logs` replays.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][0-9A-B]|[\x00-\x08\x0b\x0c\x0e-\x1f]`)

const logByteCap = 256 * 1024

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFromPath(w, r)
	if sess == nil {
		return
	}
	lines := 200
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 2000 {
			lines = n
		}
	}
	var out []byte
	var err error
	switch {
	case sess.ID != "": // supervisor session → claude logs (raw ANSI replay)
		out, err = s.Claude.Logs(r.Context(), sess.ID, logByteCap)
		out = ansiRe.ReplaceAll(out, nil)
	case sess.TmuxPaneID != "": // tmux session → capture-pane
		out, err = s.Tmux.CapturePane(r.Context(), sess.TmuxPaneID, lines, false)
	default:
		writeErr(w, http.StatusNotFound, "no_log_source", "session has neither a background id nor a tmux pane")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, "logs_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(out)
}

// handleEvents is the SSE stream: full snapshots (self-contained, so
// Last-Event-ID is ignored), throttled to ≤1/s, heartbeat comment every 15s.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "no_flush", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	send := func(snap *model.Snapshot) bool {
		data, err := json.Marshal(snap)
		if err != nil {
			return false
		}
		_, err = fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
		fl.Flush()
		return err == nil
	}

	if snap := s.Store.Get(); snap != nil && !send(snap) {
		return
	}

	sub, cancel := s.Store.Subscribe()
	defer cancel()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	throttle := time.NewTicker(time.Second)
	defer throttle.Stop()

	var pending *model.Snapshot
	for {
		select {
		case <-r.Context().Done():
			return
		case snap, open := <-sub:
			if !open {
				return
			}
			pending = snap // coalesce: only the latest matters
		case <-throttle.C:
			if pending != nil {
				if !send(pending) {
					return
				}
				pending = nil
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// --- actions ------------------------------------------------------------

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	var req actions.DispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	res, err := s.Actions.Dispatch(r.Context(), s.Store.Get(), req)
	if err != nil {
		mapActionErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFromPath(w, r)
	if sess == nil {
		return
	}
	if err := s.Actions.Stop(r.Context(), sess); err != nil {
		mapActionErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "stopped", "key": sess.Key})
}

func (s *Server) handleRespawn(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFromPath(w, r)
	if sess == nil {
		return
	}
	if err := s.Actions.Respawn(r.Context(), sess); err != nil {
		mapActionErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "respawned", "key": sess.Key})
}

func (s *Server) handleOpenInTmux(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFromPath(w, r)
	if sess == nil {
		return
	}
	res, err := s.Actions.OpenInTmux(r.Context(), sess)
	if err != nil {
		mapActionErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.Actions.ExperimentalReply {
		writeErr(w, http.StatusNotImplemented, "reply_disabled",
			"experimental reply is off (actions.experimental_reply) — use open-in-tmux and attach: see docs/REPLY.md")
		return
	}
	sess := s.sessionFromPath(w, r)
	if sess == nil {
		return
	}
	if sess.ID == "" {
		writeErr(w, http.StatusBadRequest, "bad_target", "reply requires a background session id")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if err := s.Actions.Reply(r.Context(), sess.ID, body.Text); err != nil {
		mapActionErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "delivered", "key": sess.Key})
}
