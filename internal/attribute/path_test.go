package attribute

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func TestResolveProcessPathUsesDirFDAndContainerSymlinks(t *testing.T) {
	procRoot := t.TempDir()
	pid := 4242
	pidDir := filepath.Join(procRoot, "4242")
	root := filepath.Join(pidDir, "root")
	for _, dir := range []string{"app/data", "run", "var", "proc-fd"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "run/credential"), []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app/config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/run", filepath.Join(root, "var/run")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pidDir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/app", filepath.Join(pidDir, "cwd")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/app/data", filepath.Join(pidDir, "fd/7")); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		dirFD int32
		raw   string
		want  string
	}{
		{name: "cwd relative symlink", dirFD: model.ATFDCWD, raw: "../var/run/credential", want: "/run/credential"},
		{name: "dirfd relative", dirFD: 7, raw: "../config.json", want: "/app/config.json"},
		{name: "absolute container path", dirFD: 99, raw: "/var/run/credential", want: "/run/credential"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveProcessPath(procRoot, pid, tc.dirFD, tc.raw)
			if !ok || got != tc.want {
				t.Fatalf("resolveProcessPath(%d, %q) = %q, %v; want %q, true", tc.dirFD, tc.raw, got, ok, tc.want)
			}
		})
	}
}

func TestResolveProcessPathMarksUnknownDirFDFailOpen(t *testing.T) {
	procRoot := t.TempDir()
	pidDir := filepath.Join(procRoot, "4242")
	if err := os.MkdirAll(filepath.Join(pidDir, "root"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := resolveProcessPath(procRoot, 4242, 99, "secret.txt")
	if ok || got != "secret.txt" {
		t.Fatalf("got %q, %v; want raw path and fail-open", got, ok)
	}
}

func TestDetectionAndDenyUseSameCanonicalFileBehavior(t *testing.T) {
	procRoot := t.TempDir()
	pid := 5252
	pidDir := filepath.Join(procRoot, "5252")
	root := filepath.Join(pidDir, "root")
	for _, dir := range []string{"app", "run", "var", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "run/credential"), []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/run", filepath.Join(root, "var/run")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/app", filepath.Join(pidDir, "cwd")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pidDir, "status"), "Name:\tnode\nNSpid:\t5252\n")
	writeFile(t, filepath.Join(pidDir, "maps"), "")
	writeFile(t, filepath.Join(pidDir, "cgroup"), "0::/user.slice\n")
	writeFile(t, filepath.Join(root, "tmp/perf-5252.map"), "")

	resolver := NewResolver(procRoot)
	detected := &model.RawEvent{PID: uint32(pid), TID: 1, DirFD: model.ATFDCWD, Type: uint8(model.EventFileOpen)}
	copy(detected.Arg[:], "../var/run/credential")
	denied := &model.RawEvent{PID: uint32(pid), TID: 1, DirFD: model.ATFDCWD, Type: uint8(model.EventDenyFileOpen)}
	copy(denied.Arg[:], "/run/credential")

	detectedBehavior := resolver.Attribute(detected, 0).Behavior
	deniedBehavior := resolver.Attribute(denied, 0).Behavior
	if detectedBehavior != "READ /run/credential" || deniedBehavior != detectedBehavior {
		t.Fatalf("detected=%q denied=%q", detectedBehavior, deniedBehavior)
	}
}
