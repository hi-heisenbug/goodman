package coverage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCgroupIDsForPodUID(t *testing.T) {
	root := t.TempDir()
	podUID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	needle := "poda1b2c3d4_e5f6_7890_abcd_ef1234567890"
	podDir := filepath.Join(root, "kubepods.slice", "kubepods-burstable.slice", needle+".slice")
	if err := os.MkdirAll(podDir, 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(podDir, "cri-container.scope")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	ids, err := CgroupIDsForPodUID(root, podUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) < 2 {
		t.Fatalf("want >=2 cgroup ids, got %v", ids)
	}
}

func TestResolveCgroupPaths(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "child")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	m := ResolveCgroupPaths([]string{root})
	if len(m) < 2 {
		t.Fatalf("want parent+child ids, got %v", m)
	}
}
