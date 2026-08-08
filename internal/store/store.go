// Package store holds the current fleet snapshot in memory, stamps versions,
// and fans updates out to subscribers (SSE clients, the notify engine, TUI).
// No database — the supervisor owns durable state.
package store

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/mjraval/clauditor/internal/model"
)

// Store is the single writer for fleet state.
type Store struct {
	mu      sync.RWMutex
	current *model.Snapshot
	version uint64

	subMu sync.Mutex
	subs  map[int]chan *model.Snapshot
	nextID int

	// SnapshotFile, when set, receives the latest snapshot as JSON
	// (written atomically via rename) for debugging.
	SnapshotFile string

	health struct {
		claude, tmux, git time.Time
	}
}

// New creates an empty store.
func New() *Store {
	return &Store{subs: map[int]chan *model.Snapshot{}}
}

// Set installs a new snapshot, stamping Version and notifying subscribers.
func (st *Store) Set(s *model.Snapshot) {
	st.mu.Lock()
	st.version++
	s.Version = st.version
	st.current = s
	file := st.SnapshotFile
	st.mu.Unlock()

	if file != "" {
		writeSnapshotFile(file, s)
	}

	st.subMu.Lock()
	for _, ch := range st.subs {
		select {
		case ch <- s:
		default: // slow subscriber: drop; snapshots are self-contained
		}
	}
	st.subMu.Unlock()
}

// Get returns the current snapshot (nil before the first Set).
func (st *Store) Get() *model.Snapshot {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.current
}

// Subscribe returns a channel receiving every future snapshot and an
// unsubscribe function. The channel has a buffer of 1; laggards miss
// intermediate snapshots, never block the store.
func (st *Store) Subscribe() (<-chan *model.Snapshot, func()) {
	st.subMu.Lock()
	defer st.subMu.Unlock()
	id := st.nextID
	st.nextID++
	ch := make(chan *model.Snapshot, 1)
	st.subs[id] = ch
	return ch, func() {
		st.subMu.Lock()
		defer st.subMu.Unlock()
		if c, ok := st.subs[id]; ok {
			delete(st.subs, id)
			close(c)
		}
	}
}

// MarkCollector records a collector success time for /healthz.
func (st *Store) MarkCollector(name string, t time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	switch name {
	case "claude":
		st.health.claude = t
	case "tmux":
		st.health.tmux = t
	case "git":
		st.health.git = t
	}
}

// CollectorAges returns seconds since each collector's last success
// (-1 = never).
func (st *Store) CollectorAges(now time.Time) map[string]int64 {
	st.mu.RLock()
	defer st.mu.RUnlock()
	age := func(t time.Time) int64 {
		if t.IsZero() {
			return -1
		}
		return int64(now.Sub(t).Seconds())
	}
	return map[string]int64{
		"claude": age(st.health.claude),
		"tmux":   age(st.health.tmux),
		"git":    age(st.health.git),
	}
}

func writeSnapshotFile(path string, s *model.Snapshot) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}
