package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/goodman-sec/goodman/internal/model"
	"github.com/goodman-sec/goodman/internal/store"
)

func TestListAlertsEnrichesBaselineBehaviors(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	baseline := map[string]model.BehaviorStat{
		"CONNECT 10.0.0.5:5432":              {Count: 3, FirstSeen: 1, LastSeen: 3},
		"READ /app/node_modules/good-pkg/**": {Count: 4, FirstSeen: 1, LastSeen: 4},
	}
	if err := st.UpsertFingerprint(ctx, &model.Fingerprint{
		Service: "web", Package: "good-pkg", Version: "1.0.0",
		Behaviors: baseline, FirstSeen: 1, LastSeen: 4, ObsCount: 7, IsBaseline: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertAlert(ctx, &model.Alert{
		ID: "alert-1", Service: "web", Package: "good-pkg",
		OldVersion: "1.0.0", NewVersion: "1.0.1",
		Severity:     model.SeverityCritical,
		NewBehaviors: []string{"READ /tmp/secret"},
		DetectedAt:   5,
		Status:       model.AlertOpen,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/alerts?status=open", nil)
	rec := httptest.NewRecorder()
	NewServer(st, nil, nil).Router(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var alerts []model.Alert
	if err := json.Unmarshal(rec.Body.Bytes(), &alerts); err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(alerts))
	}
	if got := alerts[0].BaselineBehaviors; !sameStrings(got, []string{
		"CONNECT 10.0.0.5:5432",
		"READ /app/node_modules/good-pkg/**",
	}) {
		t.Fatalf("baseline_behaviors = %v", got)
	}
}

func sameStrings(got, want []string) bool {
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
