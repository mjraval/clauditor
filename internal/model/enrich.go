package model

import "github.com/mjraval/clauditor/internal/collect"

// EnrichPeerReachable marks sessions reachable via Claude Code's
// cross-session messaging (docs/MESSAGING.md §4.1). It is a POST-correlation
// enrichment step, not a state source: the supervisor (`claude agents
// --json`) remains the sole authority for working/blocked/done state, and
// this only sets a supplementary boolean by matching the presence registry
// to already-correlated sessions on SessionID.
//
// A session is peer-reachable when the registry has an entry for its
// SessionID carrying both a non-empty messaging socket path and a positive
// peerProtocol version. Sessions with no registry match, or whose registry
// entry lacks either signal, are left PeerReachable=false (its zero value).
func EnrichPeerReachable(snap *Snapshot, regs []collect.SessionReg) {
	if snap == nil || len(regs) == 0 {
		return
	}
	reachable := make(map[string]bool, len(regs))
	for _, r := range regs {
		if r.SessionID == "" {
			continue
		}
		if r.MessagingSocketPath != "" && r.PeerProtocol > 0 {
			reachable[r.SessionID] = true
		}
	}
	if len(reachable) == 0 {
		return
	}
	for _, s := range snap.Sessions {
		if s.SessionID != "" && reachable[s.SessionID] {
			s.PeerReachable = true
		}
	}
}
