package report

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hi-heisenbug/goodman/internal/model"
)

type fakeStore struct {
	fps    []model.Fingerprint
	saved  map[string]string
	osv    map[string]bool
	nowSet map[string]uint64
}

func (f *fakeStore) ListFingerprints(_ context.Context, service, _ string) ([]model.Fingerprint, error) {
	var out []model.Fingerprint
	for _, fp := range f.fps {
		if service == "" || fp.Service == service {
			out = append(out, fp)
		}
	}
	return out, nil
}

func (f *fakeStore) SaveReport(_ context.Context, service, reportJSON string, osv bool, at uint64) error {
	f.saved[service] = reportJSON
	f.osv[service] = osv
	f.nowSet[service] = at
	return nil
}

func TestRefreshAll(t *testing.T) {
	st := &fakeStore{
		fps: []model.Fingerprint{
			{Service: "web", Package: "express", Version: "4.17.1",
				Behaviors: map[string]model.BehaviorStat{"READ /a": {}}},
		},
		saved: map[string]string{}, osv: map[string]bool{}, nowSet: map[string]uint64{},
	}
	lockfiles := []Lockfile{
		{Service: "web", Content: `{"lockfileVersion":3,"packages":{
			"":{"name":"web"},
			"node_modules/express":{"version":"4.17.1"},
			"node_modules/left-pad":{"version":"1.3.0"}}}`},
	}
	n, err := RefreshAll(context.Background(), st, lockfiles, nil, 999)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("refreshed = %d, want 1", n)
	}
	if st.nowSet["web"] != 999 || st.osv["web"] {
		t.Fatalf("snapshot metadata wrong: at=%d osv=%v", st.nowSet["web"], st.osv["web"])
	}
	var rep Report
	if err := json.Unmarshal([]byte(st.saved["web"]), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.DeclaredCount != 2 || rep.ExecutedCount != 1 {
		t.Fatalf("snapshot join wrong: declared=%d executed=%d", rep.DeclaredCount, rep.ExecutedCount)
	}
}

func TestRefreshAllContinuesAfterInvalidLockfile(t *testing.T) {
	st := &fakeStore{
		saved: map[string]string{}, osv: map[string]bool{}, nowSet: map[string]uint64{},
	}
	lockfiles := []Lockfile{
		{Service: "broken", Content: `{not-json`},
		{Service: "web", Content: `{"lockfileVersion":3,"packages":{"node_modules/express":{"version":"4.17.1"}}}`},
	}

	n, err := RefreshAll(context.Background(), st, lockfiles, nil, 999)
	if n != 1 {
		t.Fatalf("refreshed = %d, want 1", n)
	}
	if err == nil || !strings.Contains(err.Error(), `service "broken"`) {
		t.Fatalf("error = %v, want broken service context", err)
	}
	if _, ok := st.saved["web"]; !ok {
		t.Fatal("valid service after corrupt lockfile was not refreshed")
	}
}

func TestRefreshAllPersistsReachabilityOnlySnapshotWhenOSVFails(t *testing.T) {
	st := &fakeStore{
		saved: map[string]string{}, osv: map[string]bool{}, nowSet: map[string]uint64{},
	}
	client := &OSVClient{
		Endpoint: "https://osv.invalid/v1/querybatch",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Body:       io.NopCloser(strings.NewReader("unavailable")),
				Header:     make(http.Header),
			}, nil
		})},
	}
	lockfiles := []Lockfile{{
		Service: "web",
		Content: `{"lockfileVersion":3,"packages":{"node_modules/express":{"version":"4.17.1"}}}`,
	}}

	n, err := RefreshAll(context.Background(), st, lockfiles, client, 999)
	if n != 1 {
		t.Fatalf("refreshed = %d, want 1", n)
	}
	if err == nil || !strings.Contains(err.Error(), `service "web" osv`) {
		t.Fatalf("error = %v, want OSV degradation context", err)
	}
	if st.osv["web"] {
		t.Fatal("OSV failure must persist osv=false, not a false all-clear")
	}
	var rep Report
	if err := json.Unmarshal([]byte(st.saved["web"]), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.DeclaredCount != 1 {
		t.Fatalf("reachability-only report declared_count = %d, want 1", rep.DeclaredCount)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOSVResolveUsesConfiguredEndpoint(t *testing.T) {
	var detailURL string
	client := &OSVClient{
		Endpoint: "https://osv-proxy.invalid/custom/querybatch?tenant=goodman",
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"results":[{"vulns":[{"id":"OSV-1"}]}]}`
			if req.Method == http.MethodGet {
				detailURL = req.URL.String()
				body = `{"summary":"proxied","database_specific":{"severity":"HIGH"}}`
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		})},
	}
	got, err := client.Query(context.Background(), []DeclaredPackage{{Name: "pkg", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if detailURL != "https://osv-proxy.invalid/custom/vulns/OSV-1" {
		t.Fatalf("detail URL = %q", detailURL)
	}
	if got["pkg@1.0.0"][0].Summary != "proxied" {
		t.Fatalf("resolved vulnerability = %+v", got)
	}
}
