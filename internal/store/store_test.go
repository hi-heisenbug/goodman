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
	if got.Origin != model.OriginLocal {
		t.Fatalf("origin = %q, want local", got.Origin)
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

// Reopening the same database must not fail on non-idempotent migrations
// (ALTER TABLE runs once, tracked in schema_migrations).
func TestMigrationsAreTrackedAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		s.Close()
	}
}

func TestUpsertAlertMergesRulesAndEvidence(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	first := &model.Alert{
		ID: "ev-1", Service: "web", Package: "pkg", NewVersion: "1.0.1",
		Severity:     model.SeverityCritical,
		NewBehaviors: []string{"READ /root/.npmrc"},
		MatchedRules: []string{"secret-read"},
		Evidence: []model.Evidence{
			{Behavior: "READ /root/.npmrc", Rules: []string{"secret-read"}, Sensor: "node-a", FirstSeen: 500},
		},
		DetectedAt: 500, Status: model.AlertOpen,
	}
	if _, err := s.UpsertAlert(ctx, first); err != nil {
		t.Fatal(err)
	}

	// Same alert id, new behavior from another sensor, plus an earlier
	// sighting of the first behavior.
	second := &model.Alert{
		ID: "ev-1", Service: "web", Package: "pkg", NewVersion: "1.0.1",
		Severity:     model.SeverityCritical,
		NewBehaviors: []string{"READ /root/.npmrc", "CONNECT 169.254.169.254:80"},
		MatchedRules: []string{"secret-read", "cloud-metadata"},
		Evidence: []model.Evidence{
			{Behavior: "READ /root/.npmrc", Rules: []string{"secret-read"}, Sensor: "node-b", FirstSeen: 400},
			{Behavior: "CONNECT 169.254.169.254:80", Rules: []string{"cloud-metadata"}, Sensor: "node-b", FirstSeen: 600},
		},
		DetectedAt: 600, Status: model.AlertOpen,
	}
	if _, err := s.UpsertAlert(ctx, second); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAlert(ctx, "ev-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.MatchedRules) != 2 {
		t.Fatalf("matched_rules = %v, want union of 2", got.MatchedRules)
	}
	if len(got.Evidence) != 2 {
		t.Fatalf("evidence = %+v, want 2 entries", got.Evidence)
	}
	for _, ev := range got.Evidence {
		if ev.Behavior == "READ /root/.npmrc" {
			if ev.FirstSeen != 400 || ev.Sensor != "node-b" {
				t.Fatalf("earliest sighting must win: %+v", ev)
			}
		}
	}
}

func TestLockfileAndReportPersistence(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	// No snapshot yet.
	if _, found, err := s.GetReport(ctx, "web"); err != nil || found {
		t.Fatalf("expected no stored report, got found=%v err=%v", found, err)
	}

	if err := s.SaveLockfile(ctx, "web", `{"lockfileVersion":3}`, 100); err != nil {
		t.Fatal(err)
	}
	// Upsert replaces, does not duplicate.
	if err := s.SaveLockfile(ctx, "web", `{"lockfileVersion":3,"x":1}`, 200); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLockfile(ctx, "", `{"lockfileVersion":2}`, 150); err != nil {
		t.Fatal(err)
	}
	lfs, err := s.ListLockfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(lfs) != 2 {
		t.Fatalf("lockfiles = %d, want 2 (web + all-scope)", len(lfs))
	}
	for _, lf := range lfs {
		if lf.Service == "web" && lf.UploadedAt != 200 {
			t.Fatalf("web lockfile not upserted: %+v", lf)
		}
	}

	if err := s.SaveReport(ctx, "web", `{"declared_count":5,"executed_count":1}`, true, 300); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.GetReport(ctx, "web")
	if err != nil || !found {
		t.Fatalf("GetReport = (found %v, err %v)", found, err)
	}
	if got.Report != `{"declared_count":5,"executed_count":1}` || !got.OSV || got.ComputedAt != 300 {
		t.Fatalf("stored report mismatch: %+v", got)
	}
	if got.PreviousReport != "" || got.PreviousComputedAt != 0 {
		t.Fatalf("first save must leave previous empty: %+v", got)
	}

	if err := s.SaveReport(ctx, "web", `{"declared_count":5,"executed_count":3}`, true, 400); err != nil {
		t.Fatal(err)
	}
	got, found, err = s.GetReport(ctx, "web")
	if err != nil || !found {
		t.Fatalf("second GetReport: found=%v err=%v", found, err)
	}
	if got.Report != `{"declared_count":5,"executed_count":3}` || got.ComputedAt != 400 {
		t.Fatalf("current not updated: %+v", got)
	}
	if got.PreviousReport != `{"declared_count":5,"executed_count":1}` || got.PreviousComputedAt != 300 {
		t.Fatalf("previous not preserved: %+v", got)
	}
}

func baselineFP(service, pkg, version string, learning bool) *model.Fingerprint {
	fp := &model.Fingerprint{
		Service: service, Package: pkg, Version: version,
		Behaviors: map[string]model.BehaviorStat{
			"READ /app/node_modules/" + pkg + "/**": {Count: 10, FirstSeen: 1, LastSeen: 2},
		},
		FirstSeen: 1, LastSeen: 2, ObsCount: 10,
		IsBaseline: !learning,
	}
	return fp
}

func TestImportFingerprintConflictMatrix(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	imported := baselineFP("web", "left-pad", "1.3.0", false)
	imported.IsBaseline = true

	outcome, err := s.ImportFingerprint(ctx, imported)
	if err != nil || outcome != ImportImported {
		t.Fatalf("fresh import = (%v, %v), want imported", outcome, err)
	}
	got, err := s.GetFingerprint(ctx, "web", "left-pad", "1.3.0")
	if err != nil || got == nil || got.Origin != model.OriginImported || !got.IsBaseline {
		t.Fatalf("imported row = %+v err=%v", got, err)
	}

	// Local learning row must not be clobbered.
	if err := s.UpsertFingerprint(ctx, baselineFP("web", "pkg-a", "1.0.0", true)); err != nil {
		t.Fatal(err)
	}
	outcome, err = s.ImportFingerprint(ctx, baselineFP("web", "pkg-a", "1.0.0", false))
	if err != nil || outcome != ImportSkippedLocal {
		t.Fatalf("local learning skip = (%v, %v)", outcome, err)
	}

	// Local baseline row must not be clobbered.
	if err := s.UpsertFingerprint(ctx, &model.Fingerprint{
		Service: "web", Package: "pkg-b", Version: "2.0.0",
		Behaviors: map[string]model.BehaviorStat{"READ /local": {Count: 1}},
		IsBaseline: true, Origin: model.OriginLocal,
	}); err != nil {
		t.Fatal(err)
	}
	replacement := baselineFP("web", "pkg-b", "2.0.0", false)
	replacement.Behaviors = map[string]model.BehaviorStat{"READ /file": {Count: 99}}
	outcome, err = s.ImportFingerprint(ctx, replacement)
	if err != nil || outcome != ImportSkippedLocal {
		t.Fatalf("local baseline skip = (%v, %v)", outcome, err)
	}
	got, _ = s.GetFingerprint(ctx, "web", "pkg-b", "2.0.0")
	if got.Behaviors["READ /local"].Count != 1 {
		t.Fatalf("local baseline mutated: %+v", got.Behaviors)
	}

	// Re-import replaces an existing imported row.
	first := baselineFP("web", "pkg-d", "4.0.0", false)
	first.IsBaseline = true
	if _, err := s.ImportFingerprint(ctx, first); err != nil {
		t.Fatal(err)
	}
	reimport := baselineFP("web", "pkg-d", "4.0.0", false)
	reimport.IsBaseline = true
	reimport.Behaviors = map[string]model.BehaviorStat{"READ /new": {Count: 5}}
	outcome, err = s.ImportFingerprint(ctx, reimport)
	if err != nil || outcome != ImportReplaced {
		t.Fatalf("imported replace = (%v, %v)", outcome, err)
	}
	got, _ = s.GetFingerprint(ctx, "web", "pkg-d", "4.0.0")
	if got.Origin != model.OriginImported || got.Behaviors["READ /new"].Count != 5 {
		t.Fatalf("replaced imported row = %+v", got)
	}

	// Non-baseline incoming rows are ignored.
	learning := baselineFP("web", "pkg-c", "3.0.0", true)
	outcome, err = s.ImportFingerprint(ctx, learning)
	if err != nil || outcome != ImportIgnoredNonBaseline {
		t.Fatalf("non-baseline ignore = (%v, %v)", outcome, err)
	}
	if got, _ := s.GetFingerprint(ctx, "web", "pkg-c", "3.0.0"); got != nil {
		t.Fatalf("non-baseline row written: %+v", got)
	}
}

func TestListBaselinesExcludesLearning(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.UpsertFingerprint(ctx, baselineFP("web", "a", "1.0.0", false)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFingerprint(ctx, baselineFP("web", "b", "1.0.0", true)); err != nil {
		t.Fatal(err)
	}
	baselines, err := s.ListBaselines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(baselines) != 1 || baselines[0].Package != "a" {
		t.Fatalf("baselines = %+v, want only promoted row", baselines)
	}
}

func TestUpsertFingerprintPreservesImportedOrigin(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fp := baselineFP("web", "pkg", "1.0.0", false)
	fp.IsBaseline = true
	if _, err := s.ImportFingerprint(ctx, fp); err != nil {
		t.Fatal(err)
	}
	fp.ObsCount = 20
	st := fp.Behaviors["READ /app/node_modules/pkg/**"]
	st.Count = 20
	fp.Behaviors["READ /app/node_modules/pkg/**"] = st
	if err := s.UpsertFingerprint(ctx, fp); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetFingerprint(ctx, "web", "pkg", "1.0.0")
	if err != nil || got.Origin != model.OriginImported || got.ObsCount != 20 {
		t.Fatalf("origin not preserved through upsert: %+v err=%v", got, err)
	}
}
