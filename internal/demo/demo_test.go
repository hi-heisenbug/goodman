package demo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/report"
	"github.com/hi-heisenbug/goodman/test/replay"
)

func TestGenerateLockfileCounts(t *testing.T) {
	data := GenerateLockfile(DeclaredCount)
	pkgs, err := report.ParseLockfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != DeclaredCount {
		t.Fatalf("declared=%d, want %d", len(pkgs), DeclaredCount)
	}
	if pkgs[0].Name != "demo-dep-0001" || pkgs[0].Version != "1.0.0" {
		t.Fatalf("first package = %+v", pkgs[0])
	}
	last := pkgs[len(pkgs)-1]
	if last.Name != "demo-dep-1400" {
		t.Fatalf("last package = %+v", last)
	}
}

func TestEventTypeFromBehavior(t *testing.T) {
	cases := []struct {
		in   string
		want model.EventType
	}{
		{"READ /app/x", model.EventFileOpen},
		{"CONNECT 1.2.3.4:443", model.EventNetConnect},
		{"EXEC /bin/sh -c true", model.EventProcExec},
		{"UNKNOWN thing", model.EventFileOpen},
	}
	for _, c := range cases {
		if got := EventTypeFromBehavior(c.in); got != c.want {
			t.Fatalf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}

func TestEventStreamMatchesCorpus(t *testing.T) {
	scenarios, err := replay.LoadScenarios()
	if err != nil {
		t.Fatal(err)
	}
	var want *replay.Scenario
	for i := range scenarios {
		if scenarios[i].Name == "event-stream" {
			want = &scenarios[i]
			break
		}
	}
	if want == nil {
		t.Fatal("event-stream scenario missing from corpus")
	}
	got := EventStreamScenario()
	if got.Service != want.Service || got.Package != want.Package {
		t.Fatalf("identity mismatch: got %s/%s want %s/%s", got.Service, got.Package, want.Service, want.Package)
	}
	if got.Baseline.Version != want.Baseline.Version || got.Malicious.Version != want.Malicious.Version {
		t.Fatalf("versions mismatch: %+v vs baseline=%s malicious=%s", got, want.Baseline.Version, want.Malicious.Version)
	}
	if strings.Join(got.Baseline.Behaviors, "|") != strings.Join(want.Baseline.Behaviors, "|") {
		t.Fatalf("baseline behaviors drifted from corpus")
	}
	if strings.Join(got.Malicious.Behaviors, "|") != strings.Join(want.Malicious.Behaviors, "|") {
		t.Fatalf("malicious behaviors drifted from corpus")
	}
	if strings.Join(got.ExpectRules, "|") != strings.Join(want.Expect.MatchedRules, "|") {
		t.Fatalf("expected rules drifted: %v vs %v", got.ExpectRules, want.Expect.MatchedRules)
	}
}

func TestSeedReachabilityPostsLockfileAndEvents(t *testing.T) {
	var gotEvents int
	var gotLockfile bool
	var persisted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/events":
			var batch model.EventBatch
			if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
				t.Errorf("decode events: %v", err)
				http.Error(w, err.Error(), 400)
				return
			}
			gotEvents += len(batch.Events)
			_ = json.NewEncoder(w).Encode(map[string]any{"ingested": len(batch.Events)})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/report":
			body, _ := io.ReadAll(r.Body)
			pkgs, err := report.ParseLockfile(body)
			if err != nil {
				t.Errorf("lockfile: %v", err)
				http.Error(w, err.Error(), 400)
				return
			}
			if len(pkgs) != DeclaredCount {
				t.Errorf("lockfile pkgs=%d", len(pkgs))
			}
			gotLockfile = true
			persisted = r.URL.Query().Get("persist") == "1"
			_ = json.NewEncoder(w).Encode(report.Report{
				DeclaredCount: DeclaredCount,
				ExecutedCount: ExecutedCount,
				Rows:          []report.PackageRow{},
				VulnRows:      []report.PackageRow{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	rep, err := SeedReachability(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if !gotLockfile || !persisted {
		t.Fatalf("report post missing: lockfile=%v persist=%v", gotLockfile, persisted)
	}
	if gotEvents < ExecutedCount {
		t.Fatalf("executed events=%d, want >= %d", gotEvents, ExecutedCount)
	}
	if rep.DeclaredCount != DeclaredCount || rep.ExecutedCount != ExecutedCount {
		t.Fatalf("report counts = %+v", rep)
	}
}

func TestGuidedScriptMentionsTabs(t *testing.T) {
	s := GuidedScript("http://127.0.0.1:8844", 12*time.Second)
	for _, needle := range []string{
		"http://127.0.0.1:8844",
		"#alerts",
		"#reachability",
		"#coverage",
		"flatmap-stream",
		"1,400",
		"240",
		"12s",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("guided script missing %q:\n%s", needle, s)
		}
	}
}
