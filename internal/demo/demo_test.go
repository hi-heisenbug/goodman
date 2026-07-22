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
	got := map[string]string{}
	for _, pkg := range pkgs {
		got[pkg.Name] = pkg.Version
	}
	for name, version := range map[string]string{
		"lodash": "4.17.20", "jsonwebtoken": "8.5.1", "axios": "0.21.1", "demo-dep-1400": "1.0.0",
	} {
		if got[name] != version {
			t.Fatalf("package %s = %q, want %q", name, got[name], version)
		}
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

func TestMiniShaiHuludMatchesCorpus(t *testing.T) {
	assertScenarioMatchesCorpus(t, "mini-shai-hulud", MiniShaiHuludScenario())
}

func TestOpenClawSkillMatchesCorpus(t *testing.T) {
	assertScenarioMatchesCorpus(t, "openclaw-skill", OpenClawSkillScenario())
}

func assertScenarioMatchesCorpus(t *testing.T, name string, got Scenario) {
	t.Helper()
	scenarios, err := replay.LoadScenarios()
	if err != nil {
		t.Fatal(err)
	}
	var want *replay.Scenario
	for i := range scenarios {
		if scenarios[i].Name == name {
			want = &scenarios[i]
			break
		}
	}
	if want == nil {
		t.Fatalf("%s scenario missing from corpus", name)
	}
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
			if r.URL.Query().Get("osv") != "1" {
				t.Error("demo report must request deterministic OSV enrichment")
			}
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
		"Mini-Shai-Hulud",
		"OpenClaw",
		"@goodman-demo/calendar-sync@1.2.3",
		"1,400",
		"240",
		"12s",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("guided script missing %q:\n%s", needle, s)
		}
	}
}

func TestOptionsURLUsesAConnectableHost(t *testing.T) {
	for _, tc := range []struct {
		name string
		host string
		want string
	}{
		{name: "IPv4 wildcard", host: "0.0.0.0", want: "http://127.0.0.1:8844"},
		{name: "IPv6 wildcard", host: "::", want: "http://[::1]:8844"},
		{name: "IPv6 loopback", host: "::1", want: "http://[::1]:8844"},
		{name: "hostname", host: "localhost", want: "http://localhost:8844"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Options{Host: tc.host, Port: "8844"}).URL(); got != tc.want {
				t.Fatalf("URL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVerifyDashboardRequiresBuiltApplicationShell(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		ok   bool
	}{
		{
			name: "built dashboard",
			body: `<!doctype html><title>Goodman — Runtime Dependency Security</title><script src="/assets/index.js"></script><div id="root"></div>`,
			ok:   true,
		},
		{name: "placeholder", body: `<!doctype html><title>Goodman</title><p>Run: make dashboard</p>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			err := verifyDashboard(context.Background(), NewClient(srv.URL))
			if got := err == nil; got != tc.ok {
				t.Fatalf("verify dashboard ok=%v, want %v (err=%v)", got, tc.ok, err)
			}
		})
	}
}
