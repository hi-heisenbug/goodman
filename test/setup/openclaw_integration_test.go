package setup

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenClawInstallOptionUsesManagedPrefixEvenWhenGlobalCLIExists(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCLI := filepath.Join(binDir, "openclaw")
	if err := os.WriteFile(fakeCLI, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", filepath.Join(repo, "scripts", "integrate-openclaw.sh"),
		"--install-openclaw", "--systemd-user", "--restart", "--dry-run")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("integration dry-run failed: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{
		"Would install OpenClaw:",
		filepath.Join(home, ".local", "share", "goodman", "openclaw", "node_modules", ".bin", "openclaw"),
		"Would run: systemctl --user restart openclaw-gateway.service",
		"./bin/goodmanctl export",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("integration dry-run missing %q:\n%s", want, text)
		}
	}
}
