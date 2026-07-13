package report

import (
	"strings"
	"testing"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func find(pkgs []DeclaredPackage, name string) (DeclaredPackage, bool) {
	for _, p := range pkgs {
		if p.Name == name {
			return p, true
		}
	}
	return DeclaredPackage{}, false
}

func TestParseLockfileV3(t *testing.T) {
	data := []byte(`{
	  "lockfileVersion": 3,
	  "packages": {
	    "": { "name": "app", "version": "1.0.0" },
	    "node_modules/left-pad": { "version": "1.3.0" },
	    "node_modules/@scope/util": { "version": "2.1.0" },
	    "node_modules/jest": { "version": "29.0.0", "dev": true },
	    "node_modules/left-pad/node_modules/nested-dep": { "version": "0.0.1" }
	  }
	}`)
	pkgs, err := ParseLockfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 4 {
		t.Fatalf("got %d packages, want 4 (root excluded): %+v", len(pkgs), pkgs)
	}
	scoped, ok := find(pkgs, "@scope/util")
	if !ok || scoped.Version != "2.1.0" {
		t.Fatalf("scoped package not parsed: %+v", pkgs)
	}
	if _, ok := find(pkgs, "nested-dep"); !ok {
		t.Fatal("nested transitive dependency missing")
	}
	jest, _ := find(pkgs, "jest")
	if !jest.Dev {
		t.Fatal("jest should be marked dev")
	}
}

func TestParseLockfileV1(t *testing.T) {
	data := []byte(`{
	  "lockfileVersion": 1,
	  "dependencies": {
	    "left-pad": { "version": "1.3.0" },
	    "express": {
	      "version": "4.18.0",
	      "dependencies": {
	        "cookie": { "version": "0.5.0" }
	      }
	    }
	  }
	}`)
	pkgs, err := ParseLockfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 3 {
		t.Fatalf("got %d packages, want 3 (incl. nested cookie): %+v", len(pkgs), pkgs)
	}
	if _, ok := find(pkgs, "cookie"); !ok {
		t.Fatal("nested v1 dependency missing")
	}
}

func TestBuildPrioritizesExecutingVulnerabilities(t *testing.T) {
	declared := []DeclaredPackage{
		{Name: "runs-vuln", Version: "1.0.0"},
		{Name: "idle-vuln", Version: "2.0.0"},
		{Name: "runs-clean", Version: "3.0.0"},
		{Name: "idle-clean", Version: "4.0.0", Dev: true},
	}
	fingerprints := []model.Fingerprint{
		{Service: "web", Package: "runs-vuln", Version: "1.0.0",
			Behaviors: map[string]model.BehaviorStat{"READ /a": {}, "CONNECT /b": {}}},
		{Service: "web", Package: "runs-clean", Version: "3.0.0",
			Behaviors: map[string]model.BehaviorStat{"READ /c": {}}},
		{Service: "other", Package: "idle-vuln", Version: "2.0.0",
			Behaviors: map[string]model.BehaviorStat{"READ /d": {}}}, // wrong service, must not count
	}
	vulns := map[string][]Vulnerability{
		"runs-vuln@1.0.0": {{ID: "GHSA-xxxx", Severity: "HIGH"}},
		"idle-vuln@2.0.0": {{ID: "GHSA-yyyy", Severity: "CRITICAL"}},
	}

	rep := Build("web", declared, fingerprints, vulns)
	if rep.DeclaredCount != 4 {
		t.Fatalf("declared = %d, want 4", rep.DeclaredCount)
	}
	if rep.ExecutedCount != 2 {
		t.Fatalf("executed = %d, want 2 (service-scoped)", rep.ExecutedCount)
	}
	if len(rep.VulnRows) != 2 {
		t.Fatalf("vuln rows = %d, want 2", len(rep.VulnRows))
	}
	// Executing vulnerable package must sort first.
	if rep.VulnRows[0].Name != "runs-vuln" || !rep.VulnRows[0].Executed {
		t.Fatalf("executing vulnerable package must rank first: %+v", rep.VulnRows)
	}
	if rep.VulnRows[1].Executed {
		t.Fatal("idle vulnerable package must rank after executing ones")
	}

	md := rep.Markdown()
	for _, want := range []string{
		"2 of 4",           // executed / declared headline
		"GHSA-xxxx",        // executing vuln listed
		"idle-clean@4.0.0", // never-executed section, dev annotated
		"(dev)",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q\n%s", want, md)
		}
	}
	// The executing vulnerable row must appear before the idle one in the table.
	if strings.Index(md, "GHSA-xxxx") > strings.Index(md, "GHSA-yyyy") {
		t.Fatal("executing vulnerability must be listed before idle one")
	}
}

func TestBuildNoVulns(t *testing.T) {
	declared := []DeclaredPackage{{Name: "left-pad", Version: "1.3.0"}}
	rep := Build("", declared, nil, nil)
	md := rep.Markdown()
	if !strings.Contains(md, "No known vulnerabilities") {
		t.Fatalf("expected no-vuln note:\n%s", md)
	}
	if !strings.Contains(md, "0 of 1") {
		t.Fatalf("expected 0 of 1 executed:\n%s", md)
	}
}
