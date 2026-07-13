package digest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/report"
	"github.com/hi-heisenbug/goodman/internal/store"
)

func TestBuildDigestWithDelta(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/d.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	if _, err := st.UpsertAlert(ctx, &model.Alert{
		ID: "a1", Service: "web", Package: "x", NewVersion: "1",
		Severity: model.SeverityCritical, NewBehaviors: []string{"READ /x"},
		DetectedAt: uint64(time.Now().UnixNano()), Status: model.AlertOpen,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveLockfile(ctx, "", `{}`, 1); err != nil {
		t.Fatal(err)
	}
	prev, _ := json.Marshal(report.Report{
		DeclaredCount: 100, ExecutedCount: 10,
		Rows: []report.PackageRow{{Name: "a", DeclaredVersion: "1", Executed: true}},
	})
	cur, _ := json.Marshal(report.Report{
		DeclaredCount: 100, ExecutedCount: 12,
		Rows: []report.PackageRow{
			{Name: "a", DeclaredVersion: "1", Executed: true},
			{Name: "b", DeclaredVersion: "1", Executed: true},
		},
		VulnRows: []report.PackageRow{
			{Name: "b", DeclaredVersion: "1", Executed: true, Vulns: []report.Vulnerability{{ID: "GHSA-1"}}},
		},
	})
	if err := st.SaveReport(ctx, "", string(prev), false, 100); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveReport(ctx, "", string(cur), false, 200); err != nil {
		t.Fatal(err)
	}

	d, err := Build(ctx, st, 5, "https://goodman.example")
	if err != nil {
		t.Fatal(err)
	}
	if d.OpenAlerts != 1 || !d.HasLockfile || d.ExecutedCount != 12 {
		t.Fatalf("digest basics = %+v", d)
	}
	if d.Delta.Executed != 2 || len(d.NewExecuted) != 1 || d.NewExecuted[0] != "b@1" {
		t.Fatalf("delta = %+v new=%v", d.Delta, d.NewExecuted)
	}
	md := d.Markdown()
	if !strings.Contains(md, "Open alerts") || !strings.Contains(md, "b@1") {
		t.Fatalf("markdown missing content: %s", md)
	}
	slack := d.slackText()
	if !strings.Contains(slack, "Open Reachability") || !strings.Contains(slack, "12 executed") {
		t.Fatalf("slack text: %s", slack)
	}
}

func TestBuildDigestNoLockfile(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/d2.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d, err := Build(context.Background(), st, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.HasLockfile || d.AlertBudget != DefaultAlertBudget {
		t.Fatalf("empty digest = %+v", d)
	}
	if !strings.Contains(d.Markdown(), "No lockfile") {
		t.Fatalf("markdown: %s", d.Markdown())
	}
}
