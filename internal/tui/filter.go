package tui

import "strings"

// StateFilter narrows the fleet list to one state bucket. Cycling order
// mirrors SPEC §11: all → needs-input → working → idle → terminal → all.
type StateFilter int

const (
	FilterAll StateFilter = iota
	FilterNeeds
	FilterWorking
	FilterIdle
	FilterTerminal
)

// String returns the bucket key ("needs", "working", "idle", "terminal") or
// "all". These are the same tokens bucketOf returns, so Matches is a
// straight comparison.
func (f StateFilter) String() string {
	switch f {
	case FilterNeeds:
		return "needs"
	case FilterWorking:
		return "working"
	case FilterIdle:
		return "idle"
	case FilterTerminal:
		return "terminal"
	default:
		return "all"
	}
}

// Label is the human-facing name shown in the header/footer.
func (f StateFilter) Label() string {
	switch f {
	case FilterNeeds:
		return "needs-input"
	case FilterWorking:
		return "working"
	case FilterIdle:
		return "idle"
	case FilterTerminal:
		return "terminal"
	default:
		return "all"
	}
}

// Next cycles to the following filter, wrapping back to FilterAll.
func (f StateFilter) Next() StateFilter {
	return (f + 1) % 5
}

// Matches reports whether a session bucket (as returned by bucketOf) passes
// this filter.
func (f StateFilter) Matches(bucket string) bool {
	return f == FilterAll || f.String() == bucket
}

// MatchesQuery is the `/` fuzzy-substring filter: case-insensitive substring
// match against the fields a human would search by. Empty query matches
// everything.
func MatchesQuery(row Row, query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	if row.Session == nil {
		return true // header rows are never filtered on their own; they survive iff a child session matches
	}
	s := row.Session
	fields := []string{s.Name, s.State, s.Repo, s.Worktree, s.WaitingFor, s.ID, s.TmuxTarget}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}
