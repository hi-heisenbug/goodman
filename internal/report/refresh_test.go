package report

import (
	"context"
	"encoding/json"
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
