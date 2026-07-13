package replay

import (
	"context"
	"path/filepath"
	"testing"
)

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, s := range got {
		seen[s]++
	}
	for _, s := range want {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}

func TestReplayCorpus(t *testing.T) {
	scenarios, err := LoadScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) < 4 {
		t.Fatalf("expected the full corpus, got %d scenarios", len(scenarios))
	}

	for _, s := range scenarios {
		t.Run(s.Name, func(t *testing.T) {
			t.Logf("%s\n%s", s.Incident, s.Summary)
			alert, err := Run(context.Background(), s, filepath.Join(t.TempDir(), s.Name+".db"))
			if err != nil {
				t.Fatal(err)
			}
			if alert == nil {
				t.Fatalf("%s: no alert raised; attack would have gone undetected", s.Name)
			}
			if alert.Severity != s.Expect.Severity {
				t.Fatalf("severity = %s, want %s", alert.Severity, s.Expect.Severity)
			}
			if alert.OldVersion != s.Expect.OldVersion || alert.NewVersion != s.Expect.NewVersion {
				t.Fatalf("version transition = %q -> %q, want %q -> %q",
					alert.OldVersion, alert.NewVersion, s.Expect.OldVersion, s.Expect.NewVersion)
			}
			if !sameSet(alert.NewBehaviors, s.Expect.NewBehaviors) {
				t.Fatalf("new_behaviors = %v, want %v", alert.NewBehaviors, s.Expect.NewBehaviors)
			}
			if !sameSet(alert.MatchedRules, s.Expect.MatchedRules) {
				t.Fatalf("matched_rules = %v, want %v", alert.MatchedRules, s.Expect.MatchedRules)
			}
			if alert.WouldBlock != s.Expect.WouldBlock {
				t.Fatalf("would_block = %v, want %v", alert.WouldBlock, s.Expect.WouldBlock)
			}
			// Evidence must name the sensor for every new behavior.
			if len(alert.Evidence) != len(s.Expect.NewBehaviors) {
				t.Fatalf("evidence entries = %d, want %d", len(alert.Evidence), len(s.Expect.NewBehaviors))
			}
			for _, ev := range alert.Evidence {
				if ev.Sensor == "" || ev.FirstSeen == 0 {
					t.Fatalf("evidence missing sensor/first_seen: %+v", ev)
				}
			}
			t.Logf("CAUGHT: [%s] %s %s->%s, rules=%v",
				alert.Severity, s.Package, alert.OldVersion, alert.NewVersion, alert.MatchedRules)
		})
	}
}
