package report

import "testing"

func TestComputeDeltaNewExecutedAndVulns(t *testing.T) {
	prev := Report{
		DeclaredCount: 100,
		ExecutedCount: 10,
		Rows: []PackageRow{
			{Name: "a", DeclaredVersion: "1.0.0", Executed: true},
			{Name: "b", DeclaredVersion: "1.0.0", Executed: true},
			{Name: "idle", DeclaredVersion: "1.0.0"},
		},
		VulnRows: []PackageRow{
			{Name: "a", DeclaredVersion: "1.0.0", Executed: true, Vulns: []Vulnerability{{ID: "GHSA-old"}}},
		},
	}
	cur := Report{
		DeclaredCount: 110,
		ExecutedCount: 12,
		Rows: []PackageRow{
			{Name: "a", DeclaredVersion: "1.0.0", Executed: true},
			{Name: "b", DeclaredVersion: "1.0.0", Executed: true},
			{Name: "c", DeclaredVersion: "2.0.0", Executed: true},
			{Name: "idle", DeclaredVersion: "1.0.0"},
		},
		VulnRows: []PackageRow{
			{Name: "a", DeclaredVersion: "1.0.0", Executed: true, Vulns: []Vulnerability{{ID: "GHSA-old"}}},
			{Name: "c", DeclaredVersion: "2.0.0", Executed: true, Vulns: []Vulnerability{{ID: "GHSA-new"}}},
			{Name: "idle-vuln", DeclaredVersion: "1.0.0", Vulns: []Vulnerability{{ID: "GHSA-idle"}}},
		},
	}
	d := ComputeDelta(cur, prev, 100)
	if d.Executed != 2 || d.Declared != 10 || d.ReachableVulns != 1 {
		t.Fatalf("numeric deltas = %+v", d)
	}
	if len(d.NewExecuted) != 1 || d.NewExecuted[0] != "c@2.0.0" {
		t.Fatalf("new executed = %v", d.NewExecuted)
	}
	if len(d.NewReachableVulns) != 1 || d.NewReachableVulns[0] != "GHSA-new" {
		t.Fatalf("new vulns = %v (idle must not count)", d.NewReachableVulns)
	}
	if d.PreviousComputedAt != 100 {
		t.Fatalf("previous_computed_at = %d", d.PreviousComputedAt)
	}
}

func TestComputeDeltaEmptyPrevious(t *testing.T) {
	d := ComputeDelta(Report{DeclaredCount: 5, ExecutedCount: 2}, Report{}, 0)
	if d.Executed != 0 || d.Declared != 0 || len(d.NewExecuted) != 0 {
		t.Fatalf("empty previous must yield zero delta, got %+v", d)
	}
}
