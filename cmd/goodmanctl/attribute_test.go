package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func TestResolveAttributeTargetAutoSelectsOnlySupportedRuntime(t *testing.T) {
	procRoot := t.TempDir()
	writeProcTarget(t, procRoot, 101, "node", "node", "server.js")
	writeProcTarget(t, procRoot, 202, "bash", "bash")

	target, err := resolveAttributeTarget(procRoot, 0)
	if err != nil {
		t.Fatal(err)
	}
	if target.PID != 101 || target.Comm != "node" || target.Command != "node server.js" {
		t.Fatalf("target = %+v", target)
	}
}

func TestResolveAttributeTargetListsCandidatesWhenSeveralRuntimesExist(t *testing.T) {
	procRoot := t.TempDir()
	writeProcTarget(t, procRoot, 101, "node", "node", "server.js")
	writeProcTarget(t, procRoot, 202, "python3", "python3", "worker.py")

	_, err := resolveAttributeTarget(procRoot, 0)
	if err == nil {
		t.Fatal("multiple runtime candidates were auto-selected")
	}
	for _, want := range []string{"multiple supported runtimes", "101", "node server.js", "202", "python3 worker.py", "-pid"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("candidate error missing %q:\n%s", want, err)
		}
	}
}

func TestResolveAttributeTargetRejectsMissingPID(t *testing.T) {
	_, err := resolveAttributeTarget(t.TempDir(), 999)
	if err == nil || !strings.Contains(err.Error(), "pid 999") {
		t.Fatalf("missing pid error = %v", err)
	}
}

func TestAttributeTargetReadinessExplainsRuntimeFlags(t *testing.T) {
	procRoot := t.TempDir()
	writeProcTarget(t, procRoot, 101, "node", "node", "server.js")
	target, err := resolveAttributeTarget(procRoot, 101)
	if err != nil {
		t.Fatal(err)
	}

	ready, hint := attributeTargetReadiness(procRoot, target)
	if ready {
		t.Fatal("target without a perf map reported ready")
	}
	for _, want := range []string{"NODE_OPTIONS", "--perf-basic-prof-only-functions", "--interpreted-frames-native-stack"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("readiness hint missing %q: %s", want, hint)
		}
	}

	perfMap := filepath.Join(procRoot, "101", "root", "tmp", "perf-101.map")
	if err := os.MkdirAll(filepath.Dir(perfMap), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(perfMap, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ready, hint = attributeTargetReadiness(procRoot, target)
	if !ready || !strings.Contains(hint, "ready") {
		t.Fatalf("ready=%v hint=%q", ready, hint)
	}
}

func TestAttributeTargetReadinessExplainsPythonPerfSupport(t *testing.T) {
	procRoot := t.TempDir()
	writeProcTarget(t, procRoot, 303, "python3", "python3", "worker.py")
	if err := os.WriteFile(filepath.Join(procRoot, "303", "environ"), []byte("PYTHONPERFSUPPORT=1\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := resolveAttributeTarget(procRoot, 303)
	if err != nil {
		t.Fatal(err)
	}

	ready, hint := attributeTargetReadiness(procRoot, target)
	if ready || !strings.Contains(hint, "PYTHONPERFSUPPORT=1 is configured") {
		t.Fatalf("ready=%v hint=%q", ready, hint)
	}
}

func TestAttributeProofIsDeterministicAndRequiresExactDependency(t *testing.T) {
	proof := newAttributeProof()
	if !proof.Record(model.Attributed{Package: "zeta", Version: "2.0.0", Behavior: "READ /tmp/z"}) {
		t.Fatal("first behavior was not reported as unique")
	}
	if proof.Record(model.Attributed{Package: "zeta", Version: "2.0.0", Behavior: "READ /tmp/z"}) {
		t.Fatal("duplicate behavior was reported as unique")
	}
	proof.Record(model.Attributed{Package: "alpha", Version: "1.0.0", Behavior: "CONNECT 127.0.0.1:80"})
	proof.Record(model.Attributed{Package: "<app>", Version: "0.1.0", Behavior: "READ /tmp/app"})

	if err := proof.Verify(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	proof.WriteSummary(&out)
	got := out.String()
	if strings.Index(got, "alpha@1.0.0") > strings.Index(got, "zeta@2.0.0") {
		t.Fatalf("summary is not sorted:\n%s", got)
	}
	for _, want := range []string{"4 events", "3 unique behaviors", "3 exact dependency events"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
	if got := proof.PassMessage(); got != "PASS: Goodman attributed real syscalls to 2 dependency identities." {
		t.Fatalf("plural pass message = %q", got)
	}
	single := newAttributeProof()
	single.Record(model.Attributed{Package: "alpha", Version: "1.0.0", Behavior: "READ /tmp/a"})
	if got := single.PassMessage(); got != "PASS: Goodman attributed real syscalls to 1 dependency identity." {
		t.Fatalf("singular pass message = %q", got)
	}
}

func TestAttributeProofFailureIsActionable(t *testing.T) {
	empty := newAttributeProof()
	if err := empty.Verify(); err == nil || !strings.Contains(err.Error(), "generate traffic") {
		t.Fatalf("empty proof error = %v", err)
	}

	unknown := newAttributeProof()
	unknown.Record(model.Attributed{Package: "<unknown>", Behavior: "READ /tmp/x"})
	unknown.Record(model.Attributed{Package: "<app>", Behavior: "READ /tmp/y"})
	unknown.Record(model.Attributed{Package: "requests", Behavior: "CONNECT 127.0.0.1:80"})
	if err := unknown.Verify(); err == nil || !strings.Contains(err.Error(), "no dependency") {
		t.Fatalf("unknown proof error = %v", err)
	}
}

func writeProcTarget(t *testing.T, root string, pid int, comm string, command ...string) {
	t.Helper()
	pidDir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(filepath.Join(pidDir, "root", "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), []byte(strings.Join(command, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "status"), []byte("NSpid:\t"+strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
