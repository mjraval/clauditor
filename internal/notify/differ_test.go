package notify

import (
	"testing"
	"time"

	"github.com/mjraval/clauditor/internal/model"
)

func snap(sessions ...*model.Session) *model.Snapshot {
	return &model.Snapshot{Sessions: sessions}
}

func sess(key, state, waitingFor string) *model.Session {
	return &model.Session{Key: key, Kind: model.KindSupervisorBG, State: state, WaitingFor: waitingFor}
}

func types(events []Event) []EventType {
	var out []EventType
	for _, e := range events {
		out = append(out, e.Type)
	}
	return out
}

func TestDiffer_Transitions(t *testing.T) {
	t0 := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		prev *model.Snapshot
		cur  *model.Snapshot
		want []EventType
	}{
		{
			name: "working to blocked emits needs_input",
			prev: snap(sess("a", model.StateWorking, "")),
			cur:  snap(sess("a", model.StateBlocked, "input needed")),
			want: []EventType{EventNeedsInput},
		},
		{
			name: "waitingFor appears without state change emits needs_input",
			prev: snap(sess("a", model.StateWorking, "")),
			cur:  snap(sess("a", model.StateWorking, "permission prompt")),
			want: []EventType{EventNeedsInput},
		},
		{
			name: "working to done emits completed",
			prev: snap(sess("a", model.StateWorking, "")),
			cur:  snap(sess("a", model.StateDone, "")),
			want: []EventType{EventCompleted},
		},
		{
			name: "blocked to done does NOT emit completed",
			prev: snap(sess("a", model.StateBlocked, "")),
			cur:  snap(sess("a", model.StateDone, "")),
			want: nil,
		},
		{
			name: "any to failed emits failed",
			prev: snap(sess("a", model.StateWorking, "")),
			cur:  snap(sess("a", model.StateFailed, "")),
			want: []EventType{EventFailed},
		},
		{
			name: "new supervisor session emits session_new",
			prev: snap(),
			cur:  snap(sess("a", model.StateWorking, "")),
			want: []EventType{EventSessionNew},
		},
		{
			name: "new blocked session emits session_new AND needs_input",
			prev: snap(),
			cur:  snap(sess("a", model.StateBlocked, "input needed")),
			want: []EventType{EventSessionNew, EventNeedsInput},
		},
		{
			name: "session disappears emits session_gone",
			prev: snap(sess("a", model.StateDone, "")),
			cur:  snap(),
			want: []EventType{EventSessionGone},
		},
		{
			name: "new tmux claude emits interactive_claude_appeared",
			prev: snap(),
			cur: snap(&model.Session{
				Key: "tmux-2-701", Kind: model.KindTmuxInteractive, State: model.StateUnknown,
			}),
			want: []EventType{EventInteractiveAppeared},
		},
		{
			name: "no change emits nothing",
			prev: snap(sess("a", model.StateWorking, "")),
			cur:  snap(sess("a", model.StateWorking, "")),
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDiffer(30 * time.Second)
			d.Seed(tt.prev)
			got := types(d.Diff(tt.cur, t0))
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestDiffer_Debounce(t *testing.T) {
	d := NewDiffer(30 * time.Second)
	t0 := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	d.Seed(snap(sess("a", model.StateWorking, "")))
	// flap: working -> blocked -> working -> blocked within the window
	ev1 := d.Diff(snap(sess("a", model.StateBlocked, "input needed")), t0)
	if len(ev1) != 1 || ev1[0].Type != EventNeedsInput {
		t.Fatalf("first flap: %v", types(ev1))
	}
	ev2 := d.Diff(snap(sess("a", model.StateWorking, "")), t0.Add(5*time.Second))
	if len(ev2) != 0 {
		t.Fatalf("recovery should be silent: %v", types(ev2))
	}
	ev3 := d.Diff(snap(sess("a", model.StateBlocked, "input needed")), t0.Add(10*time.Second))
	if len(ev3) != 0 {
		t.Fatalf("re-block within debounce must be suppressed: %v", types(ev3))
	}
	// after the window, the same transition fires again
	ev4 := d.Diff(snap(sess("a", model.StateWorking, "")), t0.Add(20*time.Second))
	_ = ev4
	ev5 := d.Diff(snap(sess("a", model.StateBlocked, "input needed")), t0.Add(45*time.Second))
	if len(ev5) != 1 {
		t.Fatalf("post-debounce re-block should fire: %v", types(ev5))
	}
}

func TestDiffer_FirstDiffEstablishesBaseline(t *testing.T) {
	d := NewDiffer(30 * time.Second)
	ev := d.Diff(snap(sess("a", model.StateBlocked, "input needed")), time.Now())
	if len(ev) != 0 {
		t.Fatalf("unseeded first diff must emit nothing, got %v", types(ev))
	}
}
