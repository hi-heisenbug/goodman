package coverage

import (
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func TestObserveIngestAttributionAndSensorHealth(t *testing.T) {
	r := NewRegistry()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	r.ObserveIngest("node-a", []model.Attributed{
		{Service: "web", Package: "express"},
		{Service: "web", Package: "<app>"},
		{Service: "pay", Package: "<unknown>"},
		{Service: "pay", Package: "<unknown>"},
	}, now)
	r.ObserveIngest("node-a", nil, now.Add(10*time.Second)) // heartbeat

	snap := r.Snapshot(now.Add(15*time.Second), 2, 1)
	if len(snap.Sensors) != 1 || snap.Sensors[0].Status != "running" {
		t.Fatalf("sensors = %+v", snap.Sensors)
	}
	if snap.Attribution.Package != 1 || snap.Attribution.App != 1 || snap.Attribution.Unknown != 2 {
		t.Fatalf("attr = %+v", snap.Attribution)
	}
	if snap.Attribution.SuccessRate < 0.24 || snap.Attribution.SuccessRate > 0.26 {
		t.Fatalf("success_rate = %v, want ~0.25", snap.Attribution.SuccessRate)
	}
	if len(snap.Attribution.TopUnknown) != 1 || snap.Attribution.TopUnknown[0].Service != "pay" {
		t.Fatalf("top_unknown = %+v", snap.Attribution.TopUnknown)
	}
	if snap.AlertBudget.AlertsLast24h != 2 || snap.AlertBudget.TargetPerDay != 5 || snap.AlertBudget.WouldBlockLast24h != 1 {
		t.Fatalf("budget = %+v", snap.AlertBudget)
	}
}

func TestStaleSensorAndNamespaceGapsFirst(t *testing.T) {
	r := NewRegistry()
	now := time.Now()
	r.ObserveIngest("old-node", []model.Attributed{{Package: "x"}}, now.Add(-5*time.Minute))
	r.SetNamespaces("old-node", []NamespaceCoverage{
		{Name: "prod", InjectLabel: true, PodsTotal: 2, PodsWithNodeOptions: 2},
		{Name: "staging", InjectLabel: false, PodsTotal: 3, PodsWithNodeOptions: 0, PodsWithout: 3},
	}, now)

	snap := r.Snapshot(now, 0, 0)
	if snap.Sensors[0].Status != "stale" {
		t.Fatalf("want stale, got %+v", snap.Sensors[0])
	}
	if snap.Namespaces[0].Name != "staging" {
		t.Fatalf("gaps should sort first: %+v", snap.Namespaces)
	}
	if snap.Namespaces[0].InjectLabel {
		t.Fatal("staging must show inject_label=false")
	}
}
