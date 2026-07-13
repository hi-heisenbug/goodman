package spool

import (
	"testing"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func ev(i int) model.Attributed {
	return model.Attributed{Service: "s", Package: "p", Behavior: "READ /x", Timestamp: uint64(i)}
}

func TestSpoolRetryDrain(t *testing.T) {
	s := New(100)
	if s.Push([]model.Attributed{ev(1), ev(2)}) != 0 {
		t.Fatal("unexpected eviction")
	}
	if s.Len() != 2 {
		t.Fatalf("len=%d", s.Len())
	}
	got := s.TakeAll()
	if len(got) != 2 || s.Len() != 0 {
		t.Fatalf("take=%d remain=%d", len(got), s.Len())
	}
	if s.Dropped() != 0 {
		t.Fatalf("dropped=%d", s.Dropped())
	}
}

func TestSpoolEvictsOldest(t *testing.T) {
	s := New(3)
	s.Push([]model.Attributed{ev(1), ev(2), ev(3)})
	n := s.Push([]model.Attributed{ev(4), ev(5)})
	if n != 2 {
		t.Fatalf("evicted=%d, want 2", n)
	}
	got := s.TakeAll()
	if len(got) != 3 || got[0].Timestamp != 3 || got[2].Timestamp != 5 {
		t.Fatalf("got=%v", got)
	}
	if s.Dropped() != 2 {
		t.Fatalf("dropped=%d", s.Dropped())
	}
}
