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

func TestBuildEnforcedCgroupScopesMapsPodNameToEveryCgroup(t *testing.T) {
	scopes := buildEnforcedCgroupScopes(
		map[string]bool{"prod": true, "dev": false},
		[]podRowWithUID{
			{Namespace: "prod", Name: "checkout-abc", Hostname: "checkout-host", UID: "uid-a"},
			{Namespace: "dev", Name: "worker-def", UID: "uid-b"},
		},
		func(_ string, uid string) ([]uint64, error) {
			if uid == "uid-a" {
				return []uint64{11, 12}, nil
			}
			return []uint64{22}, nil
		},
	)
	if len(scopes) != 2 || scopes[11] != "checkout-host" || scopes[12] != "checkout-host" {
		t.Fatalf("scopes = %+v", scopes)
	}
	if _, ok := scopes[22]; ok {
		t.Fatalf("unenforced namespace leaked into scopes: %+v", scopes)
	}
}

func TestResolveExplicitCgroupScopesRequiresService(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveExplicitCgroupScopes([]string{root}); err == nil {
		t.Fatal("bare cgroup path must be rejected without a service name")
	}
	scopes, err := ResolveExplicitCgroupScopes([]string{"workload=" + root})
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) == 0 {
		t.Fatal("explicit service scope resolved no cgroups")
	}
	for _, service := range scopes {
		if service != "workload" {
			t.Fatalf("service = %q, want workload", service)
		}
	}
}
