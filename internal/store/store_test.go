package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestFingerprintRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	fp := &model.Fingerprint{
		Service: "web", Package: "left-pad", Version: "1.3.0",
		Behaviors: map[string]model.BehaviorStat{
			"READ /app/node_modules/left-pad/**": {Count: 2, FirstSeen: 10, LastSeen: 20},
		},
		FirstSeen: 10, LastSeen: 20, ObsCount: 2,
	}
	if err := s.UpsertFingerprint(ctx, fp); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetFingerprint(ctx, "web", "left-pad", "1.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ObsCount != 2 || len(got.Behaviors) != 1 {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	// Upsert must update, not duplicate.
	fp.ObsCount = 5
	fp.IsBaseline = true
	if err := s.UpsertFingerprint(ctx, fp); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListFingerprints(ctx, "web", "left-pad")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ObsCount != 5 || !all[0].IsBaseline {
		t.Fatalf("upsert did not update in place: %+v", all)
	}

	missing, err := s.GetFingerprint(ctx, "web", "left-pad", "9.9.9")
	if err != nil || missing != nil {
		t.Fatalf("missing fingerprint = (%+v, %v), want (nil, nil)", missing, err)
	}
}

func TestLatestBaselineExcludesVersionAndNonBaselines(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	put := func(version string, lastSeen uint64, baseline bool) {
		t.Helper()
		if err := s.UpsertFingerprint(ctx, &model.Fingerprint{
			Service: "web", Package: "pkg", Version: version,
			Behaviors: map[string]model.BehaviorStat{}, LastSeen: lastSeen, IsBaseline: baseline,
		}); err != nil {
			t.Fatal(err)
		}
	}
	put("1.0.0", 100, true)
	put("1.1.0", 200, true)
	put("1.2.0", 300, false) // learning, must be ignored

	got, err := s.LatestBaseline(ctx, "web", "pkg", "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Version != "1.1.0" {
		t.Fatalf("latest baseline = %+v, want version 1.1.0", got)
	}

	// Excluding the newest baseline falls back to the older one.
	got, err = s.LatestBaseline(ctx, "web", "pkg", "1.1.0")
	if err != nil || got == nil || got.Version != "1.0.0" {
		t.Fatalf("excluded baseline = (%+v, %v), want version 1.0.0", got, err)
	}

	none, err := s.LatestBaseline(ctx, "web", "other-pkg", "1.0.0")
	if err != nil || none != nil {
		t.Fatalf("unknown package baseline = (%+v, %v), want (nil, nil)", none, err)
	}
}

func TestUpsertAlertMergesAndEscalates(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	a := &model.Alert{
		ID: "alert-1", Service: "web", Package: "pkg",
		OldVersion: "1.0.0", NewVersion: "1.0.1",
		Severity:     model.SeverityWarn,
		NewBehaviors: []string{"READ /etc/passwd"},
		DetectedAt:   100, Status: model.AlertOpen,
	}
	created, err := s.UpsertAlert(ctx, a)
	if err != nil || !created {
		t.Fatalf("first upsert = (%v, %v), want created", created, err)
	}

	// Same id again: merge behaviors, keep the higher severity.
	created, err = s.UpsertAlert(ctx, &model.Alert{
		ID: "alert-1", Service: "web", Package: "pkg",
		OldVersion: "1.0.0", NewVersion: "1.0.1",
		Severity:     model.SeverityCritical,
		NewBehaviors: []string{"READ /etc/passwd", "CONNECT 169.254.169.254:80"},
		DetectedAt:   200, Status: model.AlertOpen,
	})
	if err != nil || created {
		t.Fatalf("second upsert = (%v, %v), want merge", created, err)
	}

	got, err := s.GetAlert(ctx, "alert-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Severity != model.SeverityCritical {
		t.Fatalf("severity = %s, want CRITICAL", got.Severity)
	}
	if len(got.NewBehaviors) != 2 {
		t.Fatalf("merged behaviors = %v, want 2 unique", got.NewBehaviors)
	}

	// A later lower-severity merge must not downgrade.
	if _, err := s.UpsertAlert(ctx, &model.Alert{
		ID: "alert-1", Severity: model.SeverityInfo, NewVersion: "1.0.1",
		NewBehaviors: []string{"READ /etc/passwd"}, Status: model.AlertOpen,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetAlert(ctx, "alert-1")
	if got.Severity != model.SeverityCritical {
		t.Fatalf("severity downgraded to %s", got.Severity)
	}
}

func TestSetAlertStatusAndList(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.UpsertAlert(ctx, &model.Alert{
		ID: "alert-1", Service: "web", Package: "pkg", NewVersion: "1.0.1",
		Severity: model.SeverityWarn, NewBehaviors: []string{"EXEC /bin/sh"},
		DetectedAt: 100, Status: model.AlertOpen,
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.SetAlertStatus(ctx, "alert-1", model.AlertAcknowledged); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAlertStatus(ctx, "no-such-id", model.AlertResolved); err == nil {
		t.Fatal("unknown id must error")
	}

	acked, err := s.ListAlerts(ctx, model.AlertAcknowledged)
	if err != nil || len(acked) != 1 || acked[0].Status != model.AlertAcknowledged {
		t.Fatalf("acknowledged list = (%+v, %v)", acked, err)
	}
	open, err := s.ListAlerts(ctx, model.AlertOpen)
	if err != nil || len(open) != 0 {
		t.Fatalf("open list = (%+v, %v), want empty", open, err)
	}
}

func TestPruneResolvedAlerts(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	now := time.Now()
	old := uint64(now.Add(-48 * time.Hour).UnixNano())
	recent := uint64(now.Add(-1 * time.Hour).UnixNano())

	put := func(id, status string, detectedAt uint64) {
		t.Helper()
		if _, err := s.UpsertAlert(ctx, &model.Alert{
			ID: id, Service: "web", Package: "pkg", NewVersion: "1.0.1",
			Severity: model.SeverityWarn, NewBehaviors: []string{"EXEC /bin/sh"},
			DetectedAt: detectedAt, Status: status,
		}); err != nil {
			t.Fatal(err)
		}
	}
	put("old-resolved", model.AlertResolved, old)
	put("old-open", model.AlertOpen, old)
	put("old-acked", model.AlertAcknowledged, old)
	put("new-resolved", model.AlertResolved, recent)

	n, err := s.PruneResolvedAlerts(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned = %d, want 1", n)
	}
	for _, id := range []string{"old-open", "old-acked", "new-resolved"} {
		if a, err := s.GetAlert(ctx, id); err != nil || a == nil {
			t.Fatalf("alert %s must survive pruning (err %v)", id, err)
		}
	}
	if a, _ := s.GetAlert(ctx, "old-resolved"); a != nil {
		t.Fatal("old resolved alert must be pruned")
	}

	// A pre-epoch cutoff must be a no-op, never a mass delete.
	n, err = s.PruneResolvedAlerts(ctx, time.Unix(-1, 0))
	if err != nil || n != 0 {
		t.Fatalf("pre-epoch prune = (%d, %v), want (0, nil)", n, err)
	}
}
