package openclaw

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIntegrateOpenClawDryRunWorksWithoutOpenClaw(t *testing.T) {
	repo := repoRoot(t)
	tmp := t.TempDir()
	config := filepath.Join(tmp, "config", "openclaw.env")
	launcher := filepath.Join(tmp, "bin", "openclaw-goodman")

	out, err := runIntegration(t, repo, tmp,
		"--dry-run",
		"--k8s",
		"--namespace", "agents",
		"--selector", "app=openclaw",
		"--config", config,
		"--launcher", launcher,
		"--collector", "https://collector.example",
		"--service", "openclaw",
	)
	if err != nil {
		t.Fatalf("dry-run failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"OpenClaw CLI: not found",
		"--perf-basic-prof-only-functions --interpreted-frames-native-stack",
		"GOODMAN_COLLECTOR_URL=https://collector.example",
		"GOODMAN_SERVICE=openclaw",
		"GOODMAN_EXTRA_COMMS",
		"openclaw-gatewa",
		"./bin/collector -listen :8844",
		"sudo --preserve-env=GOODMAN_INGEST_TOKEN ./bin/sensor -collector https://collector.example",
		"kubectl -n agents get deployment -l app=openclaw -o name",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, out)
		}
	}
	for _, path := range []string{config, launcher} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("dry-run wrote %s", path)
		}
	}
}

func TestIntegrateOpenClawWritesConfigAndLauncher(t *testing.T) {
	repo := repoRoot(t)
	tmp := t.TempDir()
	fakeBin := filepath.Join(tmp, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	openclawBin := filepath.Join(fakeBin, "openclaw")
	if err := os.WriteFile(openclawBin, []byte(`#!/usr/bin/env bash
printf '%s|%s|%s\n' "${NODE_OPTIONS:-}" "${GOODMAN_SERVICE:-}" "$*" > "$OPENCLAW_CAPTURE"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(tmp, "config", "openclaw.env")
	launcher := filepath.Join(tmp, "bin", "openclaw-goodman")

	out, err := runIntegration(t, repo, tmp,
		"--config", config,
		"--launcher", launcher,
		"--collector", "http://127.0.0.1:8844",
		"--service", "openclaw",
	)
	if err != nil {
		t.Fatalf("integration failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OpenClaw CLI: "+openclawBin) {
		t.Fatalf("installed CLI was not detected:\n%s", out)
	}

	configData, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	configText := string(configData)
	for _, want := range []string{
		"GOODMAN_COLLECTOR_URL=http://127.0.0.1:8844",
		"GOODMAN_SERVICE=openclaw",
		"GOODMAN_API_TOKEN",
		"GOODMAN_INGEST_TOKEN",
		"GOODMAN_EXTRA_COMMS",
		"openclaw-gatewa",
	} {
		if !strings.Contains(configText, want) {
			t.Fatalf("config missing %q:\n%s", want, configText)
		}
	}
	checkConfig := exec.Command("bash", "-c", `source "$1"; printf '%s' "$GOODMAN_NODE_OPTIONS"`, "bash", config)
	configuredOptions, err := checkConfig.Output()
	if err != nil {
		t.Fatalf("source generated config: %v", err)
	}
	if got, want := string(configuredOptions), "--perf-basic-prof-only-functions --interpreted-frames-native-stack"; got != want {
		t.Fatalf("GOODMAN_NODE_OPTIONS = %q, want %q", got, want)
	}

	launcherData, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	launcherText := string(launcherData)
	for _, want := range []string{config, openclawBin, "exec"} {
		if !strings.Contains(launcherText, want) {
			t.Fatalf("launcher missing %q:\n%s", want, launcherText)
		}
	}
	info, err := os.Stat(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("launcher is not executable: %v", info.Mode())
	}
	configInfo, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if configInfo.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", configInfo.Mode().Perm())
	}

	capture := filepath.Join(tmp, "launcher.env")
	launch := exec.Command(launcher, "gateway", "--verbose")
	launch.Env = append(os.Environ(),
		"OPENCLAW_CAPTURE="+capture,
		"NODE_OPTIONS=--trace-warnings",
	)
	if out, err := launch.CombinedOutput(); err != nil {
		t.Fatalf("run launcher: %v\n%s", err, out)
	}
	captured, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--trace-warnings --perf-basic-prof-only-functions --interpreted-frames-native-stack",
		"|openclaw|gateway --verbose",
	} {
		if !strings.Contains(string(captured), want) {
			t.Fatalf("launcher environment missing %q: %s", want, captured)
		}
	}
}

func TestIntegrateOpenClawKubernetesPreservesExistingNodeOptions(t *testing.T) {
	repo := repoRoot(t)
	tmp := t.TempDir()
	fakeBin := filepath.Join(tmp, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(tmp, "kubectl.log")
	kubectl := filepath.Join(fakeBin, "kubectl")
	if err := os.WriteFile(kubectl, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$KUBECTL_CAPTURE"
case "$*" in
  *"get deployment -l app=openclaw -o name"*)
    echo deployment.apps/openclaw
    ;;
  *"get deployment.apps/openclaw -o json"*)
    printf '%s\n' '{"spec":{"template":{"spec":{"containers":[{"name":"gateway","env":[{"name":"NODE_OPTIONS","value":"--max-old-space-size=2048"}]}]}}}}'
    ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runIntegrationEnv(t, repo, tmp, []string{"KUBECTL_CAPTURE=" + capture},
		"--k8s", "--namespace", "agents", "--selector", "app=openclaw",
		"--config", filepath.Join(tmp, "config", "openclaw.env"),
		"--launcher", filepath.Join(tmp, "bin", "openclaw-goodman"),
	)
	if err != nil {
		t.Fatalf("kubernetes integration failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	for _, want := range []string{
		"patch deployment.apps/openclaw --type=json",
		"--max-old-space-size=2048 --perf-basic-prof-only-functions --interpreted-frames-native-stack",
		"GOODMAN_SERVICE",
		"openclaw",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("kubectl log missing %q:\n%s", want, log)
		}
	}
}

func TestIntegrateOpenClawSystemdDryRunShowsPersistentSetup(t *testing.T) {
	repo := repoRoot(t)
	tmp := t.TempDir()
	out, err := runIntegration(t, repo, tmp,
		"--dry-run", "--systemd-user", "--restart",
		"--config", filepath.Join(tmp, "config", "openclaw.env"),
		"--launcher", filepath.Join(tmp, "bin", "openclaw-goodman"),
	)
	if err != nil {
		t.Fatalf("systemd dry-run failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"openclaw-gateway.service.d/goodman.conf",
		`Environment="GOODMAN_SERVICE=openclaw"`,
		"systemctl --user daemon-reload",
		"systemctl --user restart openclaw-gateway.service",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("systemd dry-run missing %q:\n%s", want, out)
		}
	}
}

func TestIntegrateOpenClawSystemdWritesMergedDropInAndRestarts(t *testing.T) {
	repo := repoRoot(t)
	tmp := t.TempDir()
	fakeBin := filepath.Join(tmp, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "openclaw"), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	systemctlLog := filepath.Join(tmp, "systemctl.log")
	if err := os.WriteFile(filepath.Join(fakeBin, "systemctl"), []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$SYSTEMCTL_CAPTURE"
case "$*" in
  "--user cat openclaw-gateway.service") exit 0 ;;
  "--user show openclaw-gateway.service --property=Environment --value")
    echo 'OTHER=value NODE_OPTIONS=--max-old-space-size=2048'
    ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runIntegrationEnv(t, repo, tmp, []string{"SYSTEMCTL_CAPTURE=" + systemctlLog},
		"--systemd-user", "--restart",
		"--config", filepath.Join(tmp, "config", "openclaw.env"),
		"--launcher", filepath.Join(tmp, "bin", "openclaw-goodman"),
	)
	if err != nil {
		t.Fatalf("systemd integration failed: %v\n%s", err, out)
	}
	dropIn := filepath.Join(tmp, ".config", "systemd", "user", "openclaw-gateway.service.d", "goodman.conf")
	data, err := os.ReadFile(dropIn)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Environment="GOODMAN_SERVICE=openclaw"`,
		`Environment="NODE_OPTIONS=--max-old-space-size=2048 --perf-basic-prof-only-functions --interpreted-frames-native-stack"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("drop-in missing %q:\n%s", want, data)
		}
	}
	logData, err := os.ReadFile(systemctlLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--user daemon-reload",
		"--user restart openclaw-gateway.service",
	} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("systemctl log missing %q:\n%s", want, logData)
		}
	}
}

func runIntegration(t *testing.T, repo, home string, args ...string) (string, error) {
	t.Helper()
	return runIntegrationEnv(t, repo, home, nil, args...)
}

func runIntegrationEnv(t *testing.T, repo, home string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	cmdArgs := append([]string{filepath.Join(repo, "scripts", "integrate-openclaw.sh")}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	path := filepath.Join(home, "fake-bin") + string(os.PathListSeparator) + "/usr/bin:/bin"
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"PATH="+path,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
