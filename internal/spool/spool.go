// Package spool holds a bounded in-memory queue of attributed events that
// failed to reach the collector. The sensor retries on the next flush tick.
// It sits on the batching/send side only — never in front of the ring-buffer
// reader (AGENTS.md: don't block the hot path).
package spool

import (
	"sync"

	"github.com/hi-heisenbug/goodman/internal/model"
)

// Spool is a FIFO of events with a hard capacity. Pushing past capacity
// evicts the oldest events and reports how many were dropped.
type Spool struct {
	mu  sync.Mutex
	cap int
	buf []model.Attributed
}

// New returns a spool that holds at most cap events. Cap ≤ 0 uses 50_000.
func New(cap int) *Spool {
	if cap <= 0 {
		cap = 50_000
	}
	return &Spool{cap: cap, buf: make([]model.Attributed, 0, min(cap, 1024))}
}

// Push appends events, evicting from the front when over capacity.
// Returns the number of events evicted.
func (s *Spool) Push(events []model.Attributed) int {
	if len(events) == 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, events...)
	evicted := 0
	if len(s.buf) > s.cap {
		evicted = len(s.buf) - s.cap
		s.buf = append([]model.Attributed{}, s.buf[evicted:]...)
	}
	return evicted
}

// TakeAll removes and returns every buffered event.
func (s *Spool) TakeAll() []model.Attributed {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) == 0 {
		return nil
	}
	out := s.buf
	s.buf = make([]model.Attributed, 0, min(s.cap, 1024))
	return out
}

// Len returns the current depth.
func (s *Spool) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
