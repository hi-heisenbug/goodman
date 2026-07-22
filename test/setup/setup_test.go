package setup

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGoRuntimeVersionGate(t *testing.T) {
	repo := setupRepoRoot(t)
	for _, tc := range []struct {
		name      string
		version   string
		supported bool
	}{
		{name: "minimum", version: "go1.25.0", supported: true},
		{name: "newer major", version: "go2.0.0", supported: true},
		{name: "old supported before security upgrades", version: "go1.23.12", supported: false},
		{name: "malformed", version: "devel", supported: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			fakeGo := filepath.Join(bin, "go")
			body := "#!/usr/bin/env bash\nif [[ \"${1:-}\" == env && \"${2:-}\" == GOVERSION ]]; then printf '%s\\n' '" + tc.version + "'; exit 0; fi\nexit 1\n"
			if err := os.WriteFile(fakeGo, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", "source scripts/lib/runtime-versions.sh; goodman_go_is_supported")
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			err := cmd.Run()
			if got := err == nil; got != tc.supported {
				t.Fatalf("go %s supported=%v, want %v (err=%v)", tc.version, got, tc.supported, err)
			}
		})
	}
}

func TestPortableSetupDryRunIsOneCommand(t *testing.T) {
	out := runSetup(t, "demo", "--check", "--dry-run")
	for _, want := range []string{
		"portable demo",
		"go build -o bin/collector ./cmd/collector",
		"go build -o bin/goodman-demo ./cmd/goodman-demo",
		"./bin/goodman-demo -check",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("portable dry-run missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "+ make ") {
		t.Fatalf("portable setup must not require make:\n%s", out)
	}
}

func TestSetupDefaultsToDemoWhenFirstArgumentIsAnOption(t *testing.T) {
	out := runSetup(t, "--check", "--dry-run")
	if !strings.Contains(out, "./bin/goodman-demo -check") {
		t.Fatalf("default demo setup missing portable verification:\n%s", out)
	}
}

func TestLiveSetupDryRunCoversFullStack(t *testing.T) {
	out := runSetup(t, "live", "--install", "--install-openclaw", "--dry-run")
	for _, want := range []string{
		"scripts/setup.sh --with-openclaw",
		"scripts/integrate-openclaw.sh --install-openclaw",
		"test/e2e/openclaw_test.sh",
		"test/e2e/drift_test.sh",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("live dry-run missing %q:\n%s", want, out)
		}
	}
}

func TestLiveDockerSetupDryRunUsesHostKernelContract(t *testing.T) {
	out := runSetup(t, "live", "--live-backend", "docker", "--dry-run")
	for _, want := range []string{
		"deploy/docker/e2e.Dockerfile",
		"--privileged",
		"--pid=host",
		"--cgroupns=host",
		"/sys/fs/cgroup:/sys/fs/cgroup:rw",
		"/sys/kernel/tracing:/sys/kernel/tracing:rw",
		"/sys/kernel/debug:/sys/kernel/debug:rw",
		"/sys/kernel/security:/sys/kernel/security:rw",
		"goodman/e2e:local",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("live Docker dry-run missing %q:\n%s", want, out)
		}
	}
}

func TestAllSetupInstallsPrerequisitesOnce(t *testing.T) {
	out := runSetup(t, "all", "--install", "--dry-run")
	if got := strings.Count(out, "scripts/setup.sh"); got != 1 {
		t.Fatalf("all setup invoked prerequisite installer %d times, want 1:\n%s", got, out)
	}
}

func TestObserveSetupDryRunTracesARealHostPID(t *testing.T) {
	out := runSetup(t, "observe", "--pid", "4242", "--duration", "20s", "--stacks", "--live-backend", "host", "--dry-run")
	for _, want := range []string{
		"real workload attribution proof",
		"make build",
		"./bin/goodmanctl attribute",
		"-pid 4242",
		"-duration 20s",
		"-dedupe",
		"-verify",
		"-stacks",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("observe host dry-run missing %q:\n%s", want, out)
		}
	}
}

func TestObserveSetupDryRunCanUseDisposableDockerToolchain(t *testing.T) {
	out := runSetup(t, "observe", "--pid", "4242", "--live-backend", "docker", "--dry-run")
	for _, want := range []string{
		"deploy/docker/e2e.Dockerfile",
		"--privileged",
		"--pid=host",
		"--entrypoint /src/bin/goodmanctl",
		"attribute -pid 4242",
		"-dedupe",
		"-verify",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("observe Docker dry-run missing %q:\n%s", want, out)
		}
	}
}

func TestObserveSetupCanAutoDiscoverTarget(t *testing.T) {
	out := runSetup(t, "observe", "--live-backend", "host", "--dry-run")
	if strings.Contains(out, "-pid") {
		t.Fatalf("auto-discovery dry-run unexpectedly supplied a pid:\n%s", out)
	}
	if !strings.Contains(out, "./bin/goodmanctl attribute") {
		t.Fatalf("auto-discovery command missing:\n%s", out)
	}
}

func TestObserveSetupRejectsInvalidPIDBeforeBuilding(t *testing.T) {
	out, err := runSetupCommand(t, "observe", "--pid", "nope", "--dry-run")
	if err == nil {
		t.Fatal("invalid observe pid succeeded")
	}
	if !strings.Contains(out, "pid must be a positive integer") {
		t.Fatalf("invalid pid error was not actionable:\n%s", out)
	}
	if strings.Contains(out, "make build") || strings.Contains(out, "docker build") {
		t.Fatalf("invalid pid reached build steps:\n%s", out)
	}
}

func TestSetupRejectsMissingOptionValueClearly(t *testing.T) {
	out, err := runSetupCommand(t, "demo", "--backend")
	if err == nil {
		t.Fatal("missing --backend value succeeded")
	}
	if !strings.Contains(out, "--backend requires a value") {
		t.Fatalf("missing value error was not actionable:\n%s", out)
	}
}

func TestSetupRejectsInvalidPortBeforeBuilding(t *testing.T) {
	out, err := runSetupCommand(t, "demo", "--port", "not-a-port", "--dry-run")
	if err == nil {
		t.Fatal("invalid port succeeded")
	}
	if !strings.Contains(out, "port must be an integer between 1 and 65535") {
		t.Fatalf("invalid port error was not actionable:\n%s", out)
	}
	if strings.Contains(out, "go build") || strings.Contains(out, "docker build") {
		t.Fatalf("invalid port reached build steps:\n%s", out)
	}
}

func runSetup(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runSetupCommand(t, args...)
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, out)
	}
	return out
}

func runSetupCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	repo := setupRepoRoot(t)
	cmdArgs := append([]string{filepath.Join(repo, "scripts", "setup-everything.sh")}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func setupRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
