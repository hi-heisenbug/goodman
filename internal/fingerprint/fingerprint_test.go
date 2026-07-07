package fingerprint

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/goodman-sec/goodman/internal/model"
	"github.com/goodman-sec/goodman/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "fp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestIngestAggregatesAndPromotes(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	eng := NewEngine(s, LearningWindow{MinObs: 4, MinAge: time.Nanosecond})

	ev := func(b string, ts uint64) model.Attributed {
		return model.Attributed{Service: "svc", Package: "p", Version: "1", Type: model.EventFileOpen, Behavior: b, Timestamp: ts}
	}

	// First batch: two distinct behaviors, both fresh, not yet baseline.
	ups, err := eng.Ingest(ctx, []model.Attributed{ev("READ /a/**", 10), ev("READ /a/**", 11), ev("CONNECT 1.2.3.4:80", 12)})
	if err != nil {
		t.Fatal(err)
	}
	if len(ups) != 1 {
		t.Fatalf("updates = %d, want 1", len(ups))
	}
	if len(ups[0].FreshBehaviors) != 2 {
		t.Fatalf("fresh = %v, want 2 distinct", ups[0].FreshBehaviors)
	}
	if ups[0].Fingerprint.IsBaseline {
		t.Fatal("promoted too early (obs=3 < 4)")
	}

	// Second batch crosses MinObs=4 -> promotion; no NEW behaviors this time.
	ups, err = eng.Ingest(ctx, []model.Attributed{ev("READ /a/**", 20), ev("CONNECT 1.2.3.4:80", 21)})
	if err != nil {
		t.Fatal(err)
	}
	if !ups[0].JustPromoted {
		t.Fatal("expected promotion once obs>=4 and age>=MinAge")
	}
	if len(ups[0].FreshBehaviors) != 0 {
		t.Fatalf("fresh = %v, want none", ups[0].FreshBehaviors)
	}

	fp, err := s.GetFingerprint(ctx, "svc", "p", "1")
	if err != nil {
		t.Fatal(err)
	}
	if !fp.IsBaseline || fp.ObsCount != 5 || len(fp.Behaviors) != 2 {
		t.Fatalf("fp = %+v", fp)
	}
	if fp.Behaviors["READ /a/**"].Count != 3 {
		t.Fatalf("READ count = %d, want 3", fp.Behaviors["READ /a/**"].Count)
	}
	if fp.FirstSeen != 10 || fp.LastSeen != 21 {
		t.Fatalf("first/last = %d/%d", fp.FirstSeen, fp.LastSeen)
	}
}

func TestIngestSkipsEmptyPackages(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(newStore(t), LearningWindow{MinObs: 1})
	ups, err := eng.Ingest(ctx, []model.Attributed{
		{Service: "s", Package: "", Behavior: "READ /x", Timestamp: 1}, // no package -> skipped
		{Service: "s", Package: "p", Behavior: "", Timestamp: 1},        // no behavior -> skipped
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ups) != 0 {
		t.Fatalf("updates = %d, want 0 (both events skipped)", len(ups))
	}
}

func TestAgeGateBlocksPromotion(t *testing.T) {
	ctx := context.Background()
	// High age requirement: enough obs but not enough wall-clock -> no baseline.
	eng := NewEngine(newStore(t), LearningWindow{MinObs: 2, MinAge: time.Hour})
	ups, err := eng.Ingest(ctx, []model.Attributed{
		{Service: "s", Package: "p", Version: "1", Behavior: "READ /a", Timestamp: 1},
		{Service: "s", Package: "p", Version: "1", Behavior: "READ /a", Timestamp: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ups[0].Fingerprint.IsBaseline {
		t.Fatal("promoted despite age gate (span 1ns < 1h)")
	}
}
