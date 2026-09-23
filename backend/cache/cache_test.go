package cache

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pr-lens/backend/analysis"
)

func TestKey_DifferentPartsDiffer(t *testing.T) {
	a := Key("diff-a", "comments")
	b := Key("diff-b", "comments")
	if a == b {
		t.Fatal("expected different keys for different diffs")
	}
}

func TestGetSet_RoundTrip(t *testing.T) {
	s := New(time.Hour)
	events := []analysis.StreamEvent{{Type: "summary", Data: map[string]any{"text": "hi"}}}
	key := Key("d")
	s.Set(key, events)
	got, ok := s.Get(key)
	if !ok || len(got) != 1 || got[0].Type != "summary" {
		t.Fatalf("Get = %v, %v", got, ok)
	}
}

func TestGet_Expired(t *testing.T) {
	s := New(time.Millisecond)
	key := Key("d")
	s.Set(key, []analysis.StreamEvent{{Type: "done"}})
	time.Sleep(5 * time.Millisecond)
	if _, ok := s.Get(key); ok {
		t.Fatal("expected expired entry to miss")
	}
}

func TestSet_EvictsOldest(t *testing.T) {
	s := New(time.Hour)
	for i := 0; i < maxEntries; i++ {
		s.Set(Key("k", fmt.Sprintf("%d", i)), []analysis.StreamEvent{{Type: "x"}})
	}
	if s.Len() != maxEntries {
		t.Fatalf("len = %d, want %d", s.Len(), maxEntries)
	}
	s.Set(Key("overflow"), []analysis.StreamEvent{{Type: "y"}})
	if s.Len() != maxEntries {
		t.Fatalf("after eviction len = %d, want %d", s.Len(), maxEntries)
	}
}

func TestWaitForInflight_SecondWaits(t *testing.T) {
	s := New(time.Hour)
	key := Key("same")

	release, waited := s.WaitForInflight(key)
	if waited {
		t.Fatal("first claim should not wait")
	}

	var ran atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rel, didWait := s.WaitForInflight(key)
		if !didWait {
			t.Error("second claim should wait")
		}
		rel()
		ran.Add(1)
	}()

	time.Sleep(20 * time.Millisecond)
	if ran.Load() != 0 {
		t.Fatal("waiter should still be blocked")
	}
	s.Set(key, []analysis.StreamEvent{{Type: "done"}})
	release()
	wg.Wait()
	if ran.Load() != 1 {
		t.Fatal("waiter did not resume")
	}
}

func TestReplay_SkipsDone(t *testing.T) {
	var types []string
	emit := func(ev analysis.StreamEvent) error {
		types = append(types, ev.Type)
		return nil
	}
	err := Replay([]analysis.StreamEvent{
		{Type: "summary"},
		{Type: "done"},
		{Type: "risk"},
	}, emit)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 2 || types[0] != "summary" || types[1] != "risk" {
		t.Fatalf("replayed %v", types)
	}
}
