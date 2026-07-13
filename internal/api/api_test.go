package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/store"
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

func TestReportEndpoint(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "report.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// express executed; left-pad declared but never observed.
	if err := st.UpsertFingerprint(ctx, &model.Fingerprint{
		Service: "web", Package: "express", Version: "4.17.1",
		Behaviors: map[string]model.BehaviorStat{"READ /a": {Count: 1}}, IsBaseline: true,
	}); err != nil {
		t.Fatal(err)
	}

	lockfile := `{"lockfileVersion":3,"packages":{
		"":{"name":"web","version":"1.0.0"},
		"node_modules/express":{"version":"4.17.1"},
		"node_modules/left-pad":{"version":"1.3.0"}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/report?service=web", strings.NewReader(lockfile))
	rec := httptest.NewRecorder()
	NewServer(st, nil, nil).Router(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var rep struct {
		DeclaredCount int `json:"declared_count"`
		ExecutedCount int `json:"executed_count"`
		Rows          []struct {
			Name     string `json:"name"`
			Executed bool   `json:"executed"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.DeclaredCount != 2 || rep.ExecutedCount != 1 {
		t.Fatalf("declared=%d executed=%d, want 2/1", rep.DeclaredCount, rep.ExecutedCount)
	}
	got := map[string]bool{}
	for _, r := range rep.Rows {
		got[r.Name] = r.Executed
	}
	if !got["express"] || got["left-pad"] {
		t.Fatalf("reachability wrong: %v", got)
	}
}

func TestReportEndpointRejectsBadLockfile(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "report2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/report", strings.NewReader("{ not json"))
	rec := httptest.NewRecorder()
	NewServer(st, nil, nil).Router(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestReportPersistAndGet(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "persist.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertFingerprint(ctx, &model.Fingerprint{
		Service: "web", Package: "express", Version: "4.17.1",
		Behaviors: map[string]model.BehaviorStat{"READ /a": {Count: 1}}, IsBaseline: true,
	}); err != nil {
		t.Fatal(err)
	}
	router := NewServer(st, nil, nil).Router(nil)

	// Before any upload: GET returns 404.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/report?service=web", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET before upload = %d, want 404", rec.Code)
	}

	// POST with persist=1 stores the snapshot.
	lockfile := `{"lockfileVersion":3,"packages":{
		"":{"name":"web"},
		"node_modules/express":{"version":"4.17.1"},
		"node_modules/left-pad":{"version":"1.3.0"}}}`
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/report?service=web&persist=1", strings.NewReader(lockfile)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST persist = %d body=%s", rec.Code, rec.Body.String())
	}

	// GET now returns the stored snapshot envelope.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/report?service=web", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET after persist = %d", rec.Code)
	}
	var env struct {
		ComputedAt uint64 `json:"computed_at"`
		Report     struct {
			DeclaredCount int `json:"declared_count"`
			ExecutedCount int `json:"executed_count"`
		} `json:"report"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.ComputedAt == 0 {
		t.Fatal("stored snapshot missing computed_at")
	}
	if env.Report.DeclaredCount != 2 || env.Report.ExecutedCount != 1 {
		t.Fatalf("stored snapshot wrong: declared=%d executed=%d", env.Report.DeclaredCount, env.Report.ExecutedCount)
	}
}
