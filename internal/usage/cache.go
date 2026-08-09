package usage

import (
	"os"
	"sync"
	"time"

	"github.com/mjraval/clauditor/internal/transcript"
)

// Cache holds the last-computed Usage per session, keyed by the transcript
// file's size and mtime. Computing cost re-reads the whole transcript file
// — too expensive to redo on every ~5s poll tick (the poll loop must never
// hammer disk that often) — so Get only recomputes when the underlying
// file actually changed since the last call. A stale-but-cheap number
// beats a fresh one that costs a full re-read every tick.
type Cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	size    int64
	modTime time.Time
	usage   Usage
	known   bool
}

// NewCache returns an empty Cache.
func NewCache() *Cache {
	return &Cache{entries: make(map[string]cacheEntry)}
}

// Get returns cached or freshly computed usage for sessionID. ok is false
// when the session has no resolvable transcript yet (matches Compute's
// contract) — any stale cache entry for a session whose transcript
// disappeared is dropped.
func (c *Cache) Get(sessionID string) (Usage, bool) {
	path, ok := transcript.Resolve(sessionID)
	if !ok {
		c.mu.Lock()
		delete(c.entries, sessionID)
		c.mu.Unlock()
		return Usage{}, false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return Usage{}, false
	}

	c.mu.Lock()
	if e, found := c.entries[sessionID]; found && e.size == fi.Size() && e.modTime.Equal(fi.ModTime()) {
		c.mu.Unlock()
		return e.usage, e.known
	}
	c.mu.Unlock()

	u, known := ComputeFile(path)
	c.mu.Lock()
	c.entries[sessionID] = cacheEntry{size: fi.Size(), modTime: fi.ModTime(), usage: u, known: known}
	c.mu.Unlock()
	return u, known
}
