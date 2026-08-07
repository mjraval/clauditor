// Package tui implements `clauditor tui`: a minimal bubbletea fleet view
// (SPEC §11). This file holds the pure row-derivation logic — building the
// same bucket-then-repo-then-worktree grouping the WebUI board uses
// (web/static/app.js BUCKETS/groupByRepoWorktree) — kept out of the tea
// Model so it's unit-testable without a TTY.
package tui

import (
	"fmt"
	"path/filepath"

	"github.com/rishi/clauditor/internal/model"
)

// RowKind distinguishes header rows (not selectable/navigable) from session
// rows (selectable).
type RowKind int

const (
	RowBucket RowKind = iota
	RowRepo
	RowWorktree
	RowSession
)

// Row is one line in the flattened, navigable fleet list.
type Row struct {
	Kind    RowKind
	Bucket  string // set on RowBucket: "needs" | "working" | "idle" | "terminal"
	Text    string // header label for RowBucket/RowRepo
	Branch  string // set on RowWorktree
	Dirty   bool
	Managed bool
	Session *model.Session // set on RowSession, nil otherwise
}

// Selectable reports whether cursor navigation may land on this row.
func (r Row) Selectable() bool { return r.Kind == RowSession }

// bucketOrder is the WebUI board's priority order (SPEC §10/§11): needs
// input always on top, then working, then idle/interactive, then terminal
// states last.
var bucketOrder = []string{"needs", "working", "idle", "terminal"}

var bucketTitle = map[string]string{
	"needs":    "NEEDS INPUT",
	"working":  "WORKING",
	"idle":     "IDLE / INTERACTIVE",
	"terminal": "DONE / FAILED / STOPPED",
}

// bucketOf classifies a session into one of the four board buckets. Mirrors
// web/static/app.js's BUCKETS predicates exactly.
func bucketOf(s *model.Session) string {
	switch {
	case s.NeedsInput():
		return "needs"
	case s.State == model.StateWorking:
		return "working"
	case s.State == model.StateIdle, s.State == model.StateUnknown:
		return "idle"
	default:
		return "terminal"
	}
}

// BuildRows flattens a snapshot into the navigable row list: for each state
// bucket (in priority order) that the state filter allows, group its
// sessions by repo → worktree (repo/worktree order follows snap.Repos, which
// Correlate already produced deterministically). Sessions failing the query
// filter are dropped; repos/worktrees left empty by filtering are omitted
// entirely. Within a worktree, session order is whatever Correlate/
// model.SortSessions already produced — this function does not reorder it.
func BuildRows(snap *model.Snapshot, query string, filter StateFilter) []Row {
	if snap == nil {
		return nil
	}
	var rows []Row
	for _, bucket := range bucketOrder {
		if !filter.Matches(bucket) {
			continue
		}
		var groupRows []Row
		count := 0
		for _, repo := range snap.Repos {
			var repoRows []Row
			for _, wt := range repo.Worktrees {
				var sessRows []Row
				for _, s := range wt.Sessions {
					if bucketOf(s) != bucket {
						continue
					}
					row := Row{Kind: RowSession, Session: s}
					if !MatchesQuery(row, query) {
						continue
					}
					sessRows = append(sessRows, row)
				}
				if len(sessRows) == 0 {
					continue
				}
				branch := wt.Branch
				if branch == "" {
					branch = filepath.Base(wt.Path)
				}
				if repo.Name == model.LooseRepoName {
					branch = "-"
				}
				repoRows = append(repoRows, Row{
					Kind:    RowWorktree,
					Branch:  branch,
					Dirty:   wt.Dirty == "true",
					Managed: wt.ManagedBy == model.ManagedByClaudeCode,
				})
				repoRows = append(repoRows, sessRows...)
				count += len(sessRows)
			}
			if len(repoRows) == 0 {
				continue
			}
			groupRows = append(groupRows, Row{Kind: RowRepo, Text: repo.Name})
			groupRows = append(groupRows, repoRows...)
		}
		if count == 0 {
			continue
		}
		rows = append(rows, Row{Kind: RowBucket, Bucket: bucket, Text: fmt.Sprintf("%s (%d)", bucketTitle[bucket], count)})
		rows = append(rows, groupRows...)
	}
	return rows
}

// FirstSelectable returns the index of the first selectable row, or -1.
func FirstSelectable(rows []Row) int {
	for i, r := range rows {
		if r.Selectable() {
			return i
		}
	}
	return -1
}

// NextSelectable returns the next selectable row index at or after from
// (searching forward, then wrapping), or -1 if rows has none.
func NextSelectable(rows []Row, from int) int {
	n := len(rows)
	if n == 0 {
		return -1
	}
	for i := 1; i <= n; i++ {
		idx := (from + i) % n
		if rows[idx].Selectable() {
			return idx
		}
	}
	return -1
}

// PrevSelectable returns the previous selectable row index at or before
// from (searching backward, then wrapping), or -1 if rows has none.
func PrevSelectable(rows []Row, from int) int {
	n := len(rows)
	if n == 0 {
		return -1
	}
	for i := 1; i <= n; i++ {
		idx := ((from-i)%n + n) % n
		if rows[idx].Selectable() {
			return idx
		}
	}
	return -1
}

// VisibleWindow computes the [start, end) slice bounds of a height-row
// viewport so cursor stays visible, scrolling by the minimum amount (keeps
// cursor roughly centered once the list is longer than the viewport).
func VisibleWindow(total, cursor, height int) (int, int) {
	if height <= 0 || total <= height {
		return 0, total
	}
	start := cursor - height/2
	if start < 0 {
		start = 0
	}
	if start+height > total {
		start = total - height
	}
	return start, start + height
}

// ClampCursor snaps cursor onto the nearest selectable row (searching
// forward, then backward) after the row set changes shape (filter/refresh).
// Returns -1 when rows has no selectable row at all.
func ClampCursor(rows []Row, cursor int) int {
	if len(rows) == 0 {
		return -1
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if rows[cursor].Selectable() {
		return cursor
	}
	for i := cursor + 1; i < len(rows); i++ {
		if rows[i].Selectable() {
			return i
		}
	}
	for i := cursor - 1; i >= 0; i-- {
		if rows[i].Selectable() {
			return i
		}
	}
	return -1
}
