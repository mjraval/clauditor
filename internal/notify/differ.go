// Package notify diffs consecutive fleet snapshots into state-change events
// (Tier 0). The same engine backs `clauditor notify` and future webhooks.
package notify

import (
	"time"

	"github.com/mjraval/clauditor/internal/model"
)

// EventType classifies a state transition.
type EventType string

const (
	EventNeedsInput           EventType = "needs_input"
	EventCompleted            EventType = "completed"
	EventFailed               EventType = "failed"
	EventSessionNew           EventType = "session_new"
	EventSessionGone          EventType = "session_gone"
	EventInteractiveAppeared  EventType = "interactive_claude_appeared"
)

// Event is one notification.
type Event struct {
	Type    EventType      `json:"event"`
	Session *model.Session `json:"session"`
	TS      time.Time      `json:"ts"`
}

// Differ computes events between consecutive snapshots with debounce:
// no duplicate event for the same session+type within the debounce window.
type Differ struct {
	Debounce time.Duration

	prev     map[string]*model.Session
	seeded   bool
	lastSent map[string]time.Time // "key\x00type" -> time
}

// NewDiffer creates a differ with the given debounce window.
func NewDiffer(debounce time.Duration) *Differ {
	return &Differ{
		Debounce: debounce,
		lastSent: map[string]time.Time{},
	}
}

// Seed primes the differ with a baseline snapshot without emitting events
// (used at --stream startup and by --once state files).
func (d *Differ) Seed(s *model.Snapshot) {
	d.prev = indexSessions(s)
	d.seeded = true
}

// Diff emits events for transitions between the previously seen snapshot
// and cur. The first call after construction (without Seed) emits nothing
// and just establishes the baseline.
func (d *Differ) Diff(cur *model.Snapshot, now time.Time) []Event {
	curIdx := indexSessions(cur)
	if !d.seeded {
		d.prev = curIdx
		d.seeded = true
		return nil
	}
	var events []Event
	emit := func(t EventType, s *model.Session) {
		k := s.Key + "\x00" + string(t)
		if last, ok := d.lastSent[k]; ok && now.Sub(last) < d.Debounce {
			return
		}
		d.lastSent[k] = now
		events = append(events, Event{Type: t, Session: s, TS: now})
	}

	for key, cs := range curIdx {
		ps, existed := d.prev[key]
		if !existed {
			if cs.Kind == model.KindTmuxInteractive {
				emit(EventInteractiveAppeared, cs)
			} else {
				emit(EventSessionNew, cs)
			}
			// A session can be born blocked/failed; surface it immediately.
			if cs.NeedsInput() {
				emit(EventNeedsInput, cs)
			} else if cs.State == model.StateFailed {
				emit(EventFailed, cs)
			}
			continue
		}
		// needs_input: any -> blocked, or waitingFor appears
		if cs.NeedsInput() && !ps.NeedsInput() {
			emit(EventNeedsInput, cs)
		}
		// completed: working -> done
		if cs.State == model.StateDone && ps.State == model.StateWorking {
			emit(EventCompleted, cs)
		}
		// failed: any -> failed
		if cs.State == model.StateFailed && ps.State != model.StateFailed {
			emit(EventFailed, cs)
		}
	}
	for key, ps := range d.prev {
		if _, still := curIdx[key]; !still {
			emit(EventSessionGone, ps)
		}
	}

	d.prev = curIdx
	// Trim debounce memory for sessions gone longer than the window.
	for k, t := range d.lastSent {
		if now.Sub(t) > 10*d.Debounce {
			delete(d.lastSent, k)
		}
	}
	return events
}

func indexSessions(s *model.Snapshot) map[string]*model.Session {
	idx := make(map[string]*model.Session, len(s.Sessions))
	for _, sess := range s.Sessions {
		idx[sess.Key] = sess
	}
	return idx
}
