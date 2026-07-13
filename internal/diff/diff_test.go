package diff

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/store"
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

// TestAlwaysOnRules verifies trigger 0: a high-risk behavior alerts during
// the learning window, before any baseline exists. Without this, a package
// that is malicious from day one is silently baselined (poisoning).
func TestAlwaysOnRules(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "alwayson.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fpEng := fingerprint.NewEngine(s, fingerprint.LearningWindow{MinObs: 1000, MinAge: time.Hour})
	rules, err := LoadRules("")
	if err != nil {
		t.Fatal(err)
	}
	diffEng := NewEngine(s, rules)

	// Benign learning traffic must stay silent, even connects and execs:
	// those rules are drift-only.
	ups, err := fpEng.Ingest(ctx, []model.Attributed{
		{Service: "web", Package: "fresh-pkg", Version: "1.0.0", Type: model.EventFileOpen,
			Behavior: "READ /app/node_modules/fresh-pkg/**", Timestamp: 100, Sensor: "node-a"},
		{Service: "web", Package: "fresh-pkg", Version: "1.0.0", Type: model.EventNetConnect,
			Behavior: "CONNECT 10.0.0.9:443", Timestamp: 101, Sensor: "node-a"},
		{Service: "web", Package: "fresh-pkg", Version: "1.0.0", Type: model.EventProcExec,
			Behavior: "EXEC /usr/bin/git", Timestamp: 102, Sensor: "node-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, up := range ups {
		if a, _ := diffEng.React(ctx, up); a != nil {
			t.Fatalf("benign learning traffic alerted: %+v", a)
		}
	}

	// A credential read with no baseline anywhere must alert immediately.
	ups, err = fpEng.Ingest(ctx, []model.Attributed{
		{Service: "web", Package: "fresh-pkg", Version: "1.0.0", Type: model.EventFileOpen,
			Behavior: "READ /root/.npmrc", Timestamp: 200, Sensor: "node-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var alert *model.Alert
	for _, up := range ups {
		if a, _ := diffEng.React(ctx, up); a != nil {
			alert = a
		}
	}
	if alert == nil {
		t.Fatal("always-on rule did not fire during learning")
	}
	if alert.Severity != model.SeverityCritical {
		t.Fatalf("severity = %s, want CRITICAL", alert.Severity)
	}
	if alert.OldVersion != "" {
		t.Fatalf("old_version = %q, want empty (no baseline)", alert.OldVersion)
	}
	if !sameStrings(alert.NewBehaviors, []string{"READ /root/.npmrc"}) {
		t.Fatalf("new_behaviors = %v; benign learning behaviors must not leak in", alert.NewBehaviors)
	}
	if !sameStrings(alert.MatchedRules, []string{"secret-read"}) {
		t.Fatalf("matched_rules = %v, want [secret-read]", alert.MatchedRules)
	}
	if len(alert.Evidence) != 1 || alert.Evidence[0].Sensor != "node-a" || alert.Evidence[0].FirstSeen != 200 {
		t.Fatalf("evidence = %+v, want sensor node-a first_seen 200", alert.Evidence)
	}
}

// TestRuleExcludes verifies exclude patterns suppress a match without
// disabling the rule.
func TestRuleExcludes(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(rulesPath, []byte(`[
		{"name": "secret-read", "pattern": "^READ .*(secret|\\.npmrc)", "always_on": true,
		 "exclude": ["^READ /var/run/secrets/kubernetes\\.io/"]}
	]`), 0o644); err != nil {
		t.Fatal(err)
	}
	rules, err := LoadRules(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !rules[0].matches("READ /root/.npmrc") {
		t.Fatal("rule must match a non-excluded secret read")
	}
	if rules[0].matches("READ /var/run/secrets/kubernetes.io/serviceaccount/token") {
		t.Fatal("excluded path must not match")
	}

	// Invalid exclude regexes must fail loudly, like invalid patterns.
	if err := os.WriteFile(rulesPath, []byte(`[
		{"name": "bad", "pattern": "^READ ", "exclude": ["("]}
	]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRules(rulesPath); err == nil {
		t.Fatal("invalid exclude regex must error")
	}
}

// TestDriftAlertCarriesRulesAndEvidence verifies trigger-1 alerts include the
// matched rule names and per-behavior evidence.
func TestDriftAlertCarriesRulesAndEvidence(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "evidence.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fpEng := fingerprint.NewEngine(s, fingerprint.LearningWindow{MinObs: 2, MinAge: time.Nanosecond})
	rules, err := LoadRules("")
	if err != nil {
		t.Fatal(err)
	}
	diffEng := NewEngine(s, rules)

	learn := []model.Attributed{
		{Service: "web", Package: "pkg", Version: "1.0.0", Type: model.EventFileOpen,
			Behavior: "READ /app/node_modules/pkg/**", Timestamp: 100, Sensor: "node-a"},
		{Service: "web", Package: "pkg", Version: "1.0.0", Type: model.EventFileOpen,
			Behavior: "READ /app/node_modules/pkg/**", Timestamp: 200, Sensor: "node-a"},
	}
	ups, err := fpEng.Ingest(ctx, learn)
	if err != nil {
		t.Fatal(err)
	}
	for _, up := range ups {
		if !up.JustPromoted {
			t.Fatal("expected immediate promotion with tiny window")
		}
	}

	ups, err = fpEng.Ingest(ctx, []model.Attributed{
		{Service: "web", Package: "pkg", Version: "1.0.1", Type: model.EventNetConnect,
			Behavior: "CONNECT 169.254.169.254:80", Timestamp: 300, Sensor: "node-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var alert *model.Alert
	for _, up := range ups {
		if a, _ := diffEng.React(ctx, up); a != nil {
			alert = a
		}
	}
	if alert == nil {
		t.Fatal("no drift alert")
	}
	if !sameStrings(alert.MatchedRules, []string{"cloud-metadata", "new-outbound-connect"}) {
		t.Fatalf("matched_rules = %v", alert.MatchedRules)
	}
	if len(alert.Evidence) != 1 {
		t.Fatalf("evidence = %+v, want 1 entry", alert.Evidence)
	}
	ev := alert.Evidence[0]
	if ev.Behavior != "CONNECT 169.254.169.254:80" || ev.Sensor != "node-b" || ev.FirstSeen != 300 {
		t.Fatalf("evidence = %+v", ev)
	}
	if !sameStrings(ev.Rules, []string{"cloud-metadata", "new-outbound-connect"}) {
		t.Fatalf("evidence rules = %v", ev.Rules)
	}
}

func TestWarnActionWouldBlock(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "warn.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rules, err := CompileRules([]Rule{
		{Name: "secret-read", AlwaysOn: true, Pattern: `^READ .*(secret|\.npmrc)`, Action: ActionWarn},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileRules([]Rule{{Name: "bad", Pattern: `^READ `, Action: "block"}}); err == nil {
		t.Fatal("block action must fail CompileRules")
	} else if !strings.Contains(err.Error(), `action "block" is not available yet`) {
		t.Fatalf("block error = %q, want enforcement-not-shipped message", err)
	}

	fpEng := fingerprint.NewEngine(s, fingerprint.LearningWindow{MinObs: 2, MinAge: time.Nanosecond})
	diffEng := NewEngine(s, rules)
	ups, err := fpEng.Ingest(ctx, []model.Attributed{
		{Service: "web", Package: "fresh-pkg", Version: "1.0.0", Type: model.EventFileOpen,
			Behavior: "READ /root/.npmrc", Timestamp: 200, Sensor: "node-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var alert *model.Alert
	for _, up := range ups {
		if a, _ := diffEng.React(ctx, up); a != nil {
			alert = a
		}
	}
	if alert == nil || !alert.WouldBlock {
		t.Fatalf("want WouldBlock alert, got %+v", alert)
	}
	got, err := s.GetAlert(ctx, alert.ID)
	if err != nil || got == nil || !got.WouldBlock {
		t.Fatalf("persisted WouldBlock = %+v err=%v", got, err)
	}
}
