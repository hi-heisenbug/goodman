package enforce

import (
	"fmt"
	"testing"

	"github.com/hi-heisenbug/goodman/internal/diff"
)

func TestCompileVerdictsLiterals(t *testing.T) {
	rules := []diff.Rule{
		{Name: "meta", Pattern: `^CONNECT 169\.254\.169\.254:`, AlwaysOn: true, Action: diff.ActionBlock},
		{Name: "shadow", Pattern: `^READ /etc/shadow`, AlwaysOn: true, Action: diff.ActionBlock},
		{Name: "sh", Pattern: `^EXEC /bin/sh`, AlwaysOn: true, Action: diff.ActionBlock},
		{Name: "reads", Pattern: `^READ `, Action: diff.ActionBlock},
		{Name: "connects", Pattern: `^CONNECT `, Action: diff.ActionBlock},
	}
	compiled, err := diff.CompileRules(rules)
	if err != nil {
		t.Fatal(err)
	}
	behaviors := []string{
		"READ /etc/shadow",
		"CONNECT 169.254.169.254:80",
		"EXEC /bin/sh",
		"CONNECT 1.2.3.0/24:443",
		"READ relative/path",
		"READ /app/src/**",
	}
	vs := CompileVerdicts(compiled, behaviors)
	if len(vs.Open) != 1 || vs.Open[0] != "/etc/shadow" {
		t.Fatalf("open = %v", vs.Open)
	}
	if len(vs.Connect) != 1 || vs.Connect[0].Addr != "169.254.169.254" || vs.Connect[0].Port != 80 {
		t.Fatalf("connect = %v", vs.Connect)
	}
	if len(vs.Exec) != 1 || vs.Exec[0] != "/bin/sh" {
		t.Fatalf("exec = %v", vs.Exec)
	}
	if len(vs.Skipped) != 3 {
		t.Fatalf("skipped = %v", vs.Skipped)
	}
}

func TestCompileVerdictsExcludeSuppresses(t *testing.T) {
	rules := []diff.Rule{
		{
			Name: "connect", Pattern: `^CONNECT `, Action: diff.ActionBlock,
			Exclude: []string{`^CONNECT 10\.0\.0\.5:`},
		},
	}
	compiled, err := diff.CompileRules(rules)
	if err != nil {
		t.Fatal(err)
	}
	vs := CompileVerdicts(compiled, []string{"CONNECT 10.0.0.5:5432", "CONNECT 8.8.8.8:443"})
	if len(vs.Connect) != 1 || vs.Connect[0].Addr != "8.8.8.8" {
		t.Fatalf("connect = %v", vs.Connect)
	}
}

func TestManagerTracksOnlyBoundedBlockRuleBehaviors(t *testing.T) {
	rules, err := diff.CompileRules([]diff.Rule{{
		Name: "connect", Pattern: `^CONNECT `, Action: diff.ActionBlock,
	}})
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, true)
	m.SetRules(rules)
	m.RecordBehavior("web", "READ /tmp/benign")
	if m.behaviorCount != 0 {
		t.Fatalf("non-block behavior was retained: %v", m.behaviors)
	}
	for i := 0; i < maxTrackedBehaviors+100; i++ {
		m.RecordBehavior("web", fmt.Sprintf("CONNECT 10.0.%d.%d:443", i/256, i%256))
	}
	if m.behaviorCount != maxTrackedBehaviors {
		t.Fatalf("tracked behaviors = %d, want cap %d", m.behaviorCount, maxTrackedBehaviors)
	}
}

func TestManagerIsolatesVerdictsByService(t *testing.T) {
	rules, err := diff.CompileRules([]diff.Rule{
		{Name: "reads", Pattern: `^READ `, Action: diff.ActionBlock},
		{Name: "exec", Pattern: `^EXEC `, Action: diff.ActionBlock},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, true)
	m.SetRules(rules)
	m.RecordBehavior("checkout-abc", "READ /etc/shadow")
	m.RecordBehavior("worker-def", "EXEC /bin/sh")

	_, _, verdicts := m.StateForSensor()
	if got := verdicts["checkout-abc"]; len(got.Open) != 1 || got.Open[0] != "/etc/shadow" || len(got.Exec) != 0 {
		t.Fatalf("checkout verdicts = %+v", got)
	}
	if got := verdicts["worker-def"]; len(got.Exec) != 1 || got.Exec[0] != "/bin/sh" || len(got.Open) != 0 {
		t.Fatalf("worker verdicts = %+v", got)
	}
}
