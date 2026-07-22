package setup

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestKubernetesNodeEnvPatchPreservesExistingOptions(t *testing.T) {
	patch := runNodeEnvMerge(t, `{
  "spec":{"template":{"spec":{"containers":[
    {"name":"gateway","env":[
      {"name":"NODE_OPTIONS","value":"--require=existing"},
      {"name":"GOODMAN_SERVICE","value":"legacy"}
    ]}
  ]}}}
}`)

	var operations []struct {
		Path  string `json:"path"`
		Value struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"value"`
	}
	if err := json.Unmarshal([]byte(patch), &operations); err != nil {
		t.Fatal(err)
	}
	if len(operations) != 2 {
		t.Fatalf("operations = %s", patch)
	}
	if got := operations[0].Value.Value; got != "--require=existing --perf-basic-prof-only-functions --interpreted-frames-native-stack" {
		t.Fatalf("NODE_OPTIONS = %q", got)
	}
	if operations[1].Value.Name != "GOODMAN_SERVICE" || operations[1].Value.Value != "openclaw" {
		t.Fatalf("service patch = %+v", operations[1])
	}
}

func TestKubernetesNodeEnvPatchIsIdempotent(t *testing.T) {
	patch := runNodeEnvMerge(t, `{
  "spec":{"template":{"spec":{"containers":[
    {"name":"gateway","env":[
      {"name":"NODE_OPTIONS","value":"--perf-basic-prof-only-functions --interpreted-frames-native-stack"},
      {"name":"GOODMAN_SERVICE","value":"openclaw"}
    ]}
  ]}}}
}`)
	if patch != "[]" {
		t.Fatalf("idempotent patch = %s", patch)
	}
}

func TestKubernetesNodeEnvPatchRejectsValueFrom(t *testing.T) {
	_, stderr, err := runNodeEnvMergeCommand(t, `{
  "spec":{"template":{"spec":{"containers":[
    {"name":"gateway","env":[
      {"name":"NODE_OPTIONS","valueFrom":{"secretKeyRef":{"name":"runtime","key":"node-options"}}}
    ]}
  ]}}}
}`)
	if err == nil || !strings.Contains(stderr, "valueFrom") {
		t.Fatalf("valueFrom merge err=%v stderr=%q", err, stderr)
	}
}

func runNodeEnvMerge(t *testing.T, deployment string) string {
	t.Helper()
	stdout, stderr, err := runNodeEnvMergeCommand(t, deployment)
	if err != nil {
		t.Fatalf("merge failed: %v\n%s", err, stderr)
	}
	return strings.TrimSpace(stdout)
}

func runNodeEnvMergeCommand(t *testing.T, deployment string) (string, string, error) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	cmd := exec.Command(python, filepath.Join(repo, "scripts", "merge-k8s-node-env.py"),
		"--perf-basic-prof-only-functions --interpreted-frames-native-stack", "openclaw")
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader(deployment)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.String(), stderr.String(), err
}
