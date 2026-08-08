package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mjraval/clauditor/internal/model"
)

func TestStore_VersionMonotonic(t *testing.T) {
	st := New()
	if st.Get() != nil {
		t.Fatal("empty store should return nil")
	}
	st.Set(&model.Snapshot{})
	st.Set(&model.Snapshot{})
	s := st.Get()
	if s.Version != 2 {
		t.Errorf("version = %d, want 2", s.Version)
	}
}

func TestStore_Subscribe(t *testing.T) {
	st := New()
	ch, cancel := st.Subscribe()
	defer cancel()

	st.Set(&model.Snapshot{})
	select {
	case s := <-ch:
		if s.Version != 1 {
			t.Errorf("subscriber got version %d", s.Version)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber never notified")
	}

	// A slow subscriber (buffer full) must not block Set.
	st.Set(&model.Snapshot{})
	st.Set(&model.Snapshot{})
	st.Set(&model.Snapshot{})
	if st.Get().Version != 4 {
		t.Errorf("version = %d, want 4", st.Get().Version)
	}

	cancel()
	if _, ok := <-ch; ok {
		// drain the buffered one, channel must eventually be closed
		if _, ok2 := <-ch; ok2 {
			t.Error("channel not closed after unsubscribe")
		}
	}
}

func TestStore_SnapshotFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "snap.json")
	st := New()
	st.SnapshotFile = file
	st.Set(&model.Snapshot{GeneratedAt: time.Now()})

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("snapshot file not written: %v", err)
	}
	var s model.Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("snapshot file not valid JSON: %v", err)
	}
	if s.Version != 1 {
		t.Errorf("file version = %d", s.Version)
	}
}

func TestStore_CollectorAges(t *testing.T) {
	st := New()
	now := time.Now()
	ages := st.CollectorAges(now)
	if ages["claude"] != -1 {
		t.Errorf("never-run collector should be -1, got %d", ages["claude"])
	}
	st.MarkCollector("claude", now.Add(-10*time.Second))
	ages = st.CollectorAges(now)
	if ages["claude"] != 10 {
		t.Errorf("claude age = %d, want 10", ages["claude"])
	}
}
