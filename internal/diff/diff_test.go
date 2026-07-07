package diff

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/goodman-sec/goodman/internal/fingerprint"
	"github.com/goodman-sec/goodman/internal/model"
	"github.com/goodman-sec/goodman/internal/store"
)

// TestDriftPipeline exercises store -> fingerprint -> diff end to end on
// SQLite: baseline learning, promotion, version drift, and the
// no-false-positive guarantee for behaviorally identical version bumps.
func TestDriftPipeline(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	fpEng := fingerprint.NewEngine(s, fingerprint.LearningWindow{MinObs: 10, MinAge: time.Nanosecond})
	rules, err := LoadRules("")
	if err != nil {
		t.Fatal(err)
	}
	diffEng := NewEngine(s, rules)

	mkEvents := func(version string, ts uint64, behaviors ...string) []model.Attributed {
		var evs []model.Attributed
		for i, b := range behaviors {
			evs = append(evs, model.Attributed{
				Service: "web", Package: "good-pkg", Version: version,
				Type: model.EventFileOpen, Behavior: b, Timestamp: ts + uint64(i),
			})
		}
		return evs
	}

	baseline := []string{
		"READ /app/node_modules/good-pkg/**",
		"CONNECT 10.0.0.5:5432",
	}

	// Learn v1.0.0 baseline: 6 batches x 2 events = 12 obs >= 10.
	var promoted bool
	for i := 0; i < 6; i++ {
		ups, err := fpEng.Ingest(ctx, mkEvents("1.0.0", uint64(1000+i*10), baseline...))
		if err != nil {
			t.Fatal(err)
		}
		for _, up := range ups {
			a, err := diffEng.React(ctx, up)
			if err != nil {
				t.Fatal(err)
			}
			if a != nil {
				t.Fatalf("alert fired during learning window: %+v", a)
			}
			promoted = promoted || up.JustPromoted
		}
	}
	if !promoted {
		t.Fatal("v1.0.0 never promoted to baseline")
	}

	// Version bump with IDENTICAL behavior: must NOT alert.
	ups, err := fpEng.Ingest(ctx, mkEvents("1.0.1-clean", 5000, baseline...))
	if err != nil {
		t.Fatal(err)
	}
	for _, up := range ups {
		if a, _ := diffEng.React(ctx, up); a != nil {
			t.Fatalf("false positive on behaviorally identical version bump: %+v", a)
		}
	}

	// Version bump WITH drift: secret read + metadata connect -> CRITICAL.
	drift := append(baseline,
		"READ /var/run/secrets/kubernetes.io/serviceaccount/token",
		"CONNECT 169.254.169.254:80",
	)
	ups, err = fpEng.Ingest(ctx, mkEvents("1.0.1", 6000, drift...))
	if err != nil {
		t.Fatal(err)
	}
	var alert *model.Alert
	for _, up := range ups {
		a, err := diffEng.React(ctx, up)
		if err != nil {
			t.Fatal(err)
		}
		if a != nil {
			alert = a
		}
	}
	if alert == nil {
		t.Fatal("no alert on drifted version")
	}
	if alert.Severity != model.SeverityCritical {
		t.Fatalf("severity = %s, want CRITICAL", alert.Severity)
	}
	if alert.OldVersion != "1.0.0" || alert.NewVersion != "1.0.1" {
		t.Fatalf("versions = %s -> %s", alert.OldVersion, alert.NewVersion)
	}
	if !sameStrings(alert.BaselineBehaviors, baseline) {
		t.Fatalf("baseline context = %v, want %v", alert.BaselineBehaviors, baseline)
	}
	want := map[string]bool{drift[2]: true, drift[3]: true}
	for _, b := range alert.NewBehaviors {
		if !want[b] {
			t.Fatalf("baseline behavior %q leaked into new_behaviors", b)
		}
		delete(want, b)
	}
	if len(want) != 0 {
		t.Fatalf("missing drift behaviors in alert: %v", want)
	}

	// Same drift again: merges into the same alert, no duplicates.
	ups, _ = fpEng.Ingest(ctx, mkEvents("1.0.1", 7000, drift...))
	for _, up := range ups {
		if _, err := diffEng.React(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	alerts, err := s.ListAlerts(ctx, model.AlertOpen)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("alert count = %d, want 1 (dedupe)", len(alerts))
	}

	// Same-version drift AFTER promotion: baseline v1.0.0 execs something new.
	ups, err = fpEng.Ingest(ctx, []model.Attributed{{
		Service: "web", Package: "good-pkg", Version: "1.0.0",
		Type: model.EventProcExec, Behavior: "EXEC curl", Timestamp: 8000,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var sameVer *model.Alert
	for _, up := range ups {
		if a, _ := diffEng.React(ctx, up); a != nil {
			sameVer = a
		}
	}
	if sameVer == nil || sameVer.Severity != model.SeverityCritical {
		t.Fatalf("same-version exec drift: %+v", sameVer)
	}
	if !sameStrings(sameVer.BaselineBehaviors, baseline) {
		t.Fatalf("same-version baseline context = %v, want %v", sameVer.BaselineBehaviors, baseline)
	}

	// Alert lifecycle.
	if err := s.SetAlertStatus(ctx, alert.ID, model.AlertAcknowledged); err != nil {
		t.Fatal(err)
	}
	open, _ := s.ListAlerts(ctx, model.AlertOpen)
	if len(open) != 1 { // only the same-version alert remains open
		t.Fatalf("open alerts = %d, want 1", len(open))
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	counts := map[string]int{}
	for _, s := range got {
		counts[s]++
	}
	for _, s := range want {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	return true
}
