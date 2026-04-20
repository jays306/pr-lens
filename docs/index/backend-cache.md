# cache/cache.go
DOES: versioned in-memory TTL cache keyed by SHA-256 of diff; bumping Version invalidates all entries
TYPE: Store { mu sync.RWMutex, m map[string]entry, ttl time.Duration }
SYMBOLS: New(ttl) → *Store, Key(diff) → string, Get(key) → ([]StreamEvent, bool), Set(key, events), Collect(emit) → (collected, wrappedEmit), Replay(events, emit) → error
CALLED BY: handler.Analyze
USE WHEN: replay cached analysis on cache hit; bump Version when prompt/schema changes
