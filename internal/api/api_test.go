package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/diff"
	"github.com/hi-heisenbug/goodman/internal/enforce"
	"github.com/hi-heisenbug/goodman/internal/fingerprint"
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

func TestListAlertsSupportsPagination(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "alerts-page.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := 1; i <= 3; i++ {
		if _, err := st.UpsertAlert(ctx, &model.Alert{
			ID: fmt.Sprintf("alert-%d", i), Service: "web", Package: "pkg", NewVersion: "1.0.1",
			Severity: model.SeverityWarn, NewBehaviors: []string{"READ /tmp/a"},
			DetectedAt: uint64(i), Status: model.AlertOpen,
		}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/alerts?status=open&limit=1&offset=1", nil)
	rec := httptest.NewRecorder()
	NewServer(st, nil, nil).Router(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var alerts []model.Alert
	if err := json.Unmarshal(rec.Body.Bytes(), &alerts); err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].ID != "alert-2" {
		t.Fatalf("page = %+v, want alert-2", alerts)
	}
}

func TestSnapshotReturnsOpenAlertsAndFingerprints(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for _, fp := range []*model.Fingerprint{
		{
			Service: "openclaw", Package: "@goodman-demo/calendar-sync", Version: "1.2.2",
			Behaviors:  map[string]model.BehaviorStat{"READ /skill/SKILL.md": {Count: 4}},
			IsBaseline: true,
		},
		{
			Service: "openclaw", Package: "@goodman-demo/calendar-sync", Version: "1.2.3",
			Behaviors: map[string]model.BehaviorStat{"READ /home/openclaw/.openclaw/credentials.json": {Count: 1}},
		},
	} {
		if err := st.UpsertFingerprint(ctx, fp); err != nil {
			t.Fatal(err)
		}
	}
	for _, alert := range []*model.Alert{
		{
			ID: "open-alert", Service: "openclaw", Package: "@goodman-demo/calendar-sync",
			OldVersion: "1.2.2", NewVersion: "1.2.3", Severity: model.SeverityCritical,
			NewBehaviors: []string{"READ /home/openclaw/.openclaw/credentials.json"},
			DetectedAt:   2, Status: model.AlertOpen,
		},
		{
			ID: "resolved-alert", Service: "web", Package: "old-pkg", NewVersion: "2.0.0",
			Severity: model.SeverityWarn, NewBehaviors: []string{"CONNECT 203.0.113.1:443"},
			DetectedAt: 1, Status: model.AlertResolved,
		},
	} {
		if _, err := st.UpsertAlert(ctx, alert); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	NewServer(st, nil, nil).Router(nil).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/v1/snapshot", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var snapshot struct {
		Schema       string              `json:"schema"`
		GeneratedAt  uint64              `json:"generated_at"`
		Alerts       []model.Alert       `json:"alerts"`
		Fingerprints []model.Fingerprint `json:"fingerprints"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Schema != "goodman.snapshot/v1" || snapshot.GeneratedAt == 0 {
		t.Fatalf("bad snapshot envelope: %+v", snapshot)
	}
	if len(snapshot.Alerts) != 1 || snapshot.Alerts[0].ID != "open-alert" {
		t.Fatalf("alerts = %+v, want only open-alert", snapshot.Alerts)
	}
	if !sameStrings(snapshot.Alerts[0].BaselineBehaviors, []string{"READ /skill/SKILL.md"}) {
		t.Fatalf("snapshot alert baseline = %v", snapshot.Alerts[0].BaselineBehaviors)
	}
	if len(snapshot.Fingerprints) != 2 {
		t.Fatalf("fingerprints = %d, want 2", len(snapshot.Fingerprints))
	}
}

func TestSnapshotUsesEmptyArrays(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "empty-snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	rec := httptest.NewRecorder()
	NewServer(st, nil, nil).Router(nil).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/v1/snapshot", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"alerts":[]`) || !strings.Contains(body, `"fingerprints":[]`) {
		t.Fatalf("snapshot must use empty arrays: %s", body)
	}
}

func TestExportReturnsAllPersistedState(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "export-all.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFingerprint(ctx, &model.Fingerprint{
		Service: "openclaw", Package: "@goodman-demo/calendar-sync", Version: "1.2.3",
		Behaviors: map[string]model.BehaviorStat{"READ /workspace/credentials": {Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, alert := range []*model.Alert{
		{ID: "open", Service: "openclaw", Package: "skill", Severity: model.SeverityCritical, DetectedAt: 2, Status: model.AlertOpen},
		{ID: "resolved", Service: "openclaw", Package: "skill", Severity: model.SeverityWarn, DetectedAt: 1, Status: model.AlertResolved},
	} {
		if _, err := st.UpsertAlert(ctx, alert); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SaveReport(ctx, "openclaw", `{"service":"openclaw","declared_count":1,"executed_count":1}`, false, 3); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	NewServer(st, nil, nil).Router(nil).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/v1/export", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var exported struct {
		Schema       string              `json:"schema"`
		GeneratedAt  uint64              `json:"generated_at"`
		Alerts       []model.Alert       `json:"alerts"`
		Fingerprints []model.Fingerprint `json:"fingerprints"`
		Reachability []struct {
			Service string `json:"service"`
		} `json:"reachability"`
		Coverage    json.RawMessage `json:"coverage"`
		Enforcement json.RawMessage `json:"enforcement"`
		Live        struct {
			EventsPersisted bool   `json:"events_persisted"`
			Stream          string `json:"stream"`
			Delivery        string `json:"delivery"`
		} `json:"live"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Schema != "goodman.export/v1" || exported.GeneratedAt == 0 {
		t.Fatalf("bad export envelope: %+v", exported)
	}
	if len(exported.Alerts) != 2 || len(exported.Fingerprints) != 1 {
		t.Fatalf("export counts: alerts=%d fingerprints=%d", len(exported.Alerts), len(exported.Fingerprints))
	}
	if len(exported.Reachability) != 1 || exported.Reachability[0].Service != "openclaw" {
		t.Fatalf("reachability = %+v", exported.Reachability)
	}
	if len(exported.Coverage) == 0 || len(exported.Enforcement) == 0 {
		t.Fatalf("missing coverage/enforcement: coverage=%s enforcement=%s", exported.Coverage, exported.Enforcement)
	}
	if exported.Live.EventsPersisted || exported.Live.Stream != "/v1/stream" || exported.Live.Delivery == "" {
		t.Fatalf("live contract = %+v", exported.Live)
	}
}

func TestExportIsNotCappedAtAlertListPageSize(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "export-pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for i := 0; i < 501; i++ {
		status := model.AlertOpen
		if i%2 == 0 {
			status = model.AlertResolved
		}
		if _, err := st.UpsertAlert(ctx, &model.Alert{
			ID: fmt.Sprintf("alert-%03d", i), Service: "svc", Package: "pkg",
			Severity: model.SeverityWarn, DetectedAt: 1, Status: status,
		}); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	NewServer(st, nil, nil).Router(nil).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/v1/export", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var exported struct {
		Alerts []model.Alert `json:"alerts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.Alerts) != 501 {
		t.Fatalf("alerts = %d, want 501 across multiple pages", len(exported.Alerts))
	}
	seen := make(map[string]bool, len(exported.Alerts))
	for _, alert := range exported.Alerts {
		seen[alert.ID] = true
	}
	if len(seen) != 501 {
		t.Fatalf("unique alerts = %d, want 501", len(seen))
	}
}

func TestInternalErrorsDoNotLeakDatabaseDetails(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	router := NewServer(st, nil, nil).Router(nil)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/alerts", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "internal server error\n" {
		t.Fatalf("body leaked implementation detail: %q", rec.Body.String())
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

	// Second persist after a fingerprint change creates a previous snapshot + delta.
	if err := st.UpsertFingerprint(ctx, &model.Fingerprint{
		Service: "web", Package: "left-pad", Version: "1.3.0",
		Behaviors: map[string]model.BehaviorStat{"READ /b": {Count: 1}}, IsBaseline: true,
	}); err != nil {
		t.Fatal(err)
	}
	// Seed a report older than a week so the next persisted snapshot has a
	// genuine week-over-week comparison point (hourly refreshes no longer shift
	// the delta baseline).
	if err := st.SaveReport(ctx, "web", `{"service":"web","declared_count":2,"executed_count":1,"vuln_rows":[],"rows":[]}`,
		false, uint64(time.Now().Add(-8*24*time.Hour).UnixNano())); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/report?service=web&persist=1", strings.NewReader(lockfile)))
	if rec.Code != http.StatusOK {
		t.Fatalf("second POST persist = %d", rec.Code)
	}

	// GET now returns the stored snapshot envelope with a delta.
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
		Delta *struct {
			Executed int `json:"executed"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.ComputedAt == 0 {
		t.Fatal("stored snapshot missing computed_at")
	}
	if env.Report.DeclaredCount != 2 || env.Report.ExecutedCount != 2 {
		t.Fatalf("stored snapshot wrong: declared=%d executed=%d", env.Report.DeclaredCount, env.Report.ExecutedCount)
	}
	if env.Delta == nil || env.Delta.Executed != 1 {
		t.Fatalf("expected executed delta +1, got %+v body=%s", env.Delta, rec.Body.String())
	}

	// The dashboard requests the default scope. When only a service-scoped
	// snapshot exists, the API falls back to the first stored service.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/report", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET default scope fallback = %d body=%s", rec.Code, rec.Body.String())
	}
	var fallback struct {
		Report struct {
			Service string `json:"service"`
		} `json:"report"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &fallback); err != nil {
		t.Fatal(err)
	}
	if fallback.Report.Service != "web" {
		t.Fatalf("fallback service = %q, want web", fallback.Report.Service)
	}
}

func TestStreamIsExcludedFromRequestTimeout(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "stream.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	s := NewServer(st, nil, nil)
	w := newThreadSafeFlusher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.router(nil, 20*time.Millisecond).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/stream", nil).WithContext(ctx))
		close(done)
	}()
	waitForBody(t, w, ": connected\n\n")

	time.Sleep(50 * time.Millisecond)
	s.broadcast("alert", map[string]string{"id": "still-live"})
	waitForBody(t, w, "event: alert\n")
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not stop after request cancellation")
	}
}

func TestHTTPServerSetsReadHeaderTimeout(t *testing.T) {
	srv := newHTTPServer(":0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 {
		t.Fatal("ReadHeaderTimeout must protect the collector from slowloris requests")
	}
}

func TestEnforceStateReturnsServiceScopedVerdicts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "enforce-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rules, err := diff.CompileRules([]diff.Rule{{Name: "reads", Pattern: `^READ `, Action: diff.ActionBlock}})
	if err != nil {
		t.Fatal(err)
	}
	manager := enforce.NewManager(st, true)
	manager.SetRules(rules)
	manager.RecordBehavior("checkout-abc", "READ /etc/shadow")
	manager.RecordBehavior("worker-def", "READ /var/run/worker.key")
	server := NewServer(st, nil, nil)
	server.SetEnforceManager(manager)

	rec := httptest.NewRecorder()
	server.Router(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/enforce/state", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var state struct {
		Verdicts enforce.ServiceVerdicts `json:"verdicts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Verdicts) != 2 || state.Verdicts["checkout-abc"].Open[0] != "/etc/shadow" {
		t.Fatalf("verdicts = %+v", state.Verdicts)
	}
}

type threadSafeFlusher struct {
	mu     sync.Mutex
	header http.Header
	body   bytes.Buffer
	status int
}

func newThreadSafeFlusher() *threadSafeFlusher {
	return &threadSafeFlusher{header: make(http.Header)}
}

func (w *threadSafeFlusher) Header() http.Header { return w.header }

func (w *threadSafeFlusher) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = status
}

func (w *threadSafeFlusher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(p)
}

func (w *threadSafeFlusher) Flush() {}

func (w *threadSafeFlusher) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func waitForBody(t *testing.T, w *threadSafeFlusher, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(w.String(), want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("response body never contained %q; got %q", want, w.String())
}

func TestExportFingerprintsOnlyBaselines(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "export.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFingerprint(ctx, &model.Fingerprint{
		Service: "web", Package: "baseline-pkg", Version: "1.0.0",
		Behaviors:  map[string]model.BehaviorStat{"READ /a": {Count: 1}},
		IsBaseline: true, Origin: model.OriginLocal,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertFingerprint(ctx, &model.Fingerprint{
		Service: "web", Package: "learning-pkg", Version: "1.0.0",
		Behaviors:  map[string]model.BehaviorStat{"READ /b": {Count: 1}},
		IsBaseline: false,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/fingerprints/export", nil)
	rec := httptest.NewRecorder()
	NewServer(st, nil, nil).Router(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Schema       string              `json:"schema"`
		ExportedAt   uint64              `json:"exported_at"`
		Collector    string              `json:"collector"`
		Fingerprints []model.Fingerprint `json:"fingerprints"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Schema != "goodman.fingerprints.export/v1" {
		t.Fatalf("schema = %q", env.Schema)
	}
	if env.ExportedAt == 0 || env.Collector == "" {
		t.Fatalf("missing envelope fields: %+v", env)
	}
	if len(env.Fingerprints) != 1 || env.Fingerprints[0].Package != "baseline-pkg" {
		t.Fatalf("fingerprints = %+v, want only baseline", env.Fingerprints)
	}
}

func TestImportFingerprintsValidatesSchemaAndCounts(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "import.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertFingerprint(ctx, &model.Fingerprint{
		Service: "web", Package: "local-pkg", Version: "1.0.0",
		Behaviors:  map[string]model.BehaviorStat{"READ /local": {Count: 1}},
		IsBaseline: true, Origin: model.OriginLocal,
	}); err != nil {
		t.Fatal(err)
	}
	router := NewServer(st, nil, nil).Router(nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/fingerprints/import",
		strings.NewReader(`{"schema":"wrong/v9","fingerprints":[]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad schema = %d, want 400", rec.Code)
	}

	body := `{"schema":"goodman.fingerprints.export/v1","fingerprints":[
		{"service":"web","package":"new-pkg","version":"1.0.0","behaviors":{"READ /x":{"count":1}},"is_baseline":true},
		{"service":"web","package":"local-pkg","version":"1.0.0","behaviors":{"READ /x":{"count":1}},"is_baseline":true},
		{"service":"web","package":"learning","version":"1.0.0","behaviors":{},"is_baseline":false}
	]}`
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/fingerprints/import", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("import = %d body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Imported           int `json:"imported"`
		SkippedLocal       int `json:"skipped_local"`
		IgnoredNonBaseline int `json:"ignored_non_baseline"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 || result.SkippedLocal != 1 || result.IgnoredNonBaseline != 1 {
		t.Fatalf("counts = %+v, want imported=1 skipped_local=1 ignored_non_baseline=1", result)
	}
	got, err := st.GetFingerprint(ctx, "web", "new-pkg", "1.0.0")
	if err != nil || got == nil || got.Origin != model.OriginImported {
		t.Fatalf("imported row = %+v err=%v", got, err)
	}
}

func TestExportImportDriftWithoutLearning(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	stA, err := store.Open(filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stA.Close()
	stB, err := store.Open(filepath.Join(dir, "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stB.Close()

	baseline := []string{
		"READ /app/node_modules/good-pkg/**",
		"CONNECT 10.0.0.5:5432",
	}
	behaviors := map[string]model.BehaviorStat{}
	for _, b := range baseline {
		behaviors[b] = model.BehaviorStat{Count: 12, FirstSeen: 1, LastSeen: 2}
	}
	if err := stA.UpsertFingerprint(ctx, &model.Fingerprint{
		Service: "web", Package: "good-pkg", Version: "1.0.0",
		Behaviors: behaviors, FirstSeen: 1, LastSeen: 2, ObsCount: 12,
		IsBaseline: true, Origin: model.OriginLocal,
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	NewServer(stA, nil, nil).Router(nil).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/v1/fingerprints/export", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	exportBody := rec.Body.Bytes()

	rec = httptest.NewRecorder()
	NewServer(stB, nil, nil).Router(nil).ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/v1/fingerprints/import", strings.NewReader(string(exportBody))))
	if rec.Code != http.StatusOK {
		t.Fatalf("import B = %d %s", rec.Code, rec.Body.String())
	}

	fps, err := stB.ListFingerprints(ctx, "web", "good-pkg")
	if err != nil || len(fps) != 1 || fps[0].Origin != model.OriginImported {
		t.Fatalf("imported provenance = %+v err=%v", fps, err)
	}

	fpEng := fingerprint.NewEngine(stB, fingerprint.LearningWindow{MinObs: 1000, MinAge: time.Hour})
	rules, err := diff.LoadRules("")
	if err != nil {
		t.Fatal(err)
	}
	diffEng := diff.NewEngine(stB, rules)
	drift := append(baseline,
		"READ /var/run/secrets/kubernetes.io/serviceaccount/token",
		"CONNECT 169.254.169.254:80",
	)
	var evs []model.Attributed
	for i, b := range drift {
		evs = append(evs, model.Attributed{
			Service: "web", Package: "good-pkg", Version: "1.0.1",
			Type: model.EventFileOpen, Behavior: b, Timestamp: uint64(100 + i),
		})
	}
	ups, err := fpEng.Ingest(ctx, evs)
	if err != nil {
		t.Fatal(err)
	}
	var alert *model.Alert
	for _, up := range ups {
		if a, err := diffEng.React(ctx, up); err != nil {
			t.Fatal(err)
		} else if a != nil {
			alert = a
		}
	}
	if alert == nil {
		t.Fatal("no drift alert against imported baseline")
	}
	if alert.OldVersion != "1.0.0" || alert.NewVersion != "1.0.1" {
		t.Fatalf("versions = %s -> %s", alert.OldVersion, alert.NewVersion)
	}
	if alert.Severity != model.SeverityCritical {
		t.Fatalf("severity = %s", alert.Severity)
	}
}
