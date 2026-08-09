package model

import (
	"testing"

	"github.com/mjraval/clauditor/internal/collect"
)

// TestEnrichPeerReachable_MatchesBySessionID reuses the M2 scenario fixture
// (correlate_test.go) and checks the enrichment sets PeerReachable only for
// the session whose SessionID matches a registry entry carrying both a
// socket path and a positive peerProtocol.
func TestEnrichPeerReachable_MatchesBySessionID(t *testing.T) {
	snap := Correlate(fixtureInputs())

	regs := []collect.SessionReg{
		// reachable: full socket + protocol
		{SessionID: "s-working", MessagingSocketPath: "/run/user/1000/cc-socks/501.sock", PeerProtocol: 1},
		// present in registry but no socket (matches the real fixture's
		// "interactive, messagingSocketPath: null" shape) — not reachable.
		{SessionID: "s-blocked", MessagingSocketPath: "", PeerProtocol: 1},
		// present with a socket but peerProtocol 0 — treated as not reachable.
		{SessionID: "s-done", MessagingSocketPath: "/run/user/1000/cc-socks/999.sock", PeerProtocol: 0},
		// no match in snapshot sessions at all — must be a no-op, never panic.
		{SessionID: "s-does-not-exist", MessagingSocketPath: "/run/user/1000/cc-socks/1.sock", PeerProtocol: 1},
	}

	EnrichPeerReachable(snap, regs)

	got := map[string]bool{}
	for _, s := range snap.Sessions {
		if s.SessionID != "" {
			got[s.SessionID] = s.PeerReachable
		}
	}

	if !got["s-working"] {
		t.Error("s-working: want PeerReachable=true (socket + protocol present)")
	}
	if got["s-blocked"] {
		t.Error("s-blocked: want PeerReachable=false (no socket)")
	}
	if got["s-done"] {
		t.Error("s-done: want PeerReachable=false (peerProtocol 0)")
	}
	if got["s-failed"] {
		t.Error("s-failed: want PeerReachable=false (no registry entry at all)")
	}
}

// TestEnrichPeerReachable_NilSafe: nil snapshot and empty/nil registry lists
// must never panic — the enrichment runs every poll cycle unconditionally.
func TestEnrichPeerReachable_NilSafe(t *testing.T) {
	EnrichPeerReachable(nil, nil)
	EnrichPeerReachable(nil, []collect.SessionReg{{SessionID: "x"}})

	snap := Correlate(fixtureInputs())
	EnrichPeerReachable(snap, nil)
	for _, s := range snap.Sessions {
		if s.PeerReachable {
			t.Errorf("session %s: expected untouched PeerReachable=false with nil registry", s.Key)
		}
	}
}

// TestEnrichPeerReachable_TmuxOnlySessionNeverMatches: tmux-interactive
// sessions have no SessionID (KeyFor uses pane/pid identity), so they must
// never accidentally match a registry entry via an empty-string SessionID
// collision.
func TestEnrichPeerReachable_TmuxOnlySessionNeverMatches(t *testing.T) {
	snap := Correlate(fixtureInputs())
	var tmuxOnly *Session
	for _, s := range snap.Sessions {
		if s.Kind == KindTmuxInteractive {
			tmuxOnly = s
			break
		}
	}
	if tmuxOnly == nil {
		t.Fatal("fixture must contain a tmux-only session")
	}
	if tmuxOnly.SessionID != "" {
		t.Fatalf("test assumption violated: tmux-only session has a SessionID %q", tmuxOnly.SessionID)
	}

	regs := []collect.SessionReg{
		{SessionID: "", MessagingSocketPath: "/run/user/1000/cc-socks/1.sock", PeerProtocol: 1},
	}
	EnrichPeerReachable(snap, regs)

	if tmuxOnly.PeerReachable {
		t.Error("tmux-only session (empty SessionID) must never match an empty-string registry entry")
	}
}
