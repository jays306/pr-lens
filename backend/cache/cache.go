// Package cache provides a versioned in-memory cache for PR analysis results.
//
// The cache key is derived from the cache version and the SHA-256 of the PR
// analysis inputs. Bumping Version invalidates all existing entries without
// needing to clear storage manually — old keys simply never match.
package cache

import (
	"crypto/sha256"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pr-lens/backend/analysis"
)

// Version is embedded in every cache key. Bump this whenever the prompt or
// output schema changes so stale entries are never replayed.
const Version = "v3"

// entry holds the cached events and metadata for one diff.
type entry struct {
	events    []analysis.StreamEvent
	createdAt time.Time
}

// Store is a thread-safe in-memory cache of analysis results.
type Store struct {
	mu  sync.RWMutex
	m   map[string]entry
	ttl time.Duration
}

// New returns a Store with the given TTL. Entries older than ttl are
// considered stale and re-analyzed. Use 0 to disable expiry.
func New(ttl time.Duration) *Store {
	return &Store{
		m:   make(map[string]entry),
		ttl: ttl,
	}
}

// Key returns the cache key for the current Version and input material.
func Key(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return fmt.Sprintf("%s:%x", Version, sum)
}

// Get returns the cached events for key, and whether they were found and
// still fresh.
func (s *Store) Get(key string) ([]analysis.StreamEvent, bool) {
	s.mu.RLock()
	e, ok := s.m[key]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if s.ttl > 0 && time.Since(e.createdAt) > s.ttl {
		s.mu.Lock()
		delete(s.m, key)
		s.mu.Unlock()
		return nil, false
	}
	return e.events, true
}

// Set stores events under key.
func (s *Store) Set(key string, events []analysis.StreamEvent) {
	s.mu.Lock()
	s.m[key] = entry{events: events, createdAt: time.Now()}
	s.mu.Unlock()
}

// Len returns the number of entries currently held.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

// Collect wraps an emit function, recording every event it receives so they
// can be stored in the cache after the analysis completes.
func Collect(emit func(analysis.StreamEvent) error) (collected *[]analysis.StreamEvent, wrappedEmit func(analysis.StreamEvent) error) {
	events := make([]analysis.StreamEvent, 0, 32)
	collected = &events
	wrappedEmit = func(ev analysis.StreamEvent) error {
		events = append(events, ev)
		return emit(ev)
	}
	return
}

// Replay replays cached events through emit, skipping the "done" event so the
// caller can emit it after any post-processing (e.g. logging).
func Replay(events []analysis.StreamEvent, emit func(analysis.StreamEvent) error) error {
	for _, ev := range events {
		if ev.Type == "done" {
			continue
		}
		if err := emit(ev); err != nil {
			return err
		}
	}
	return nil
}

// LogStats logs a one-line cache stats summary.
func (s *Store) LogStats(key string, hit bool) {
	status := "MISS"
	if hit {
		status = "HIT"
	}
	log.Printf("[cache] %s key=%s entries=%d version=%s", status, key[:16]+"…", s.Len(), Version)
}
