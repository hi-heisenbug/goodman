package goodman_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestHelmManifestHasRunnableSecurityContexts(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	out, err := exec.Command("helm", "template", "goodman", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	rendered := string(out)
	for _, want := range []string{
		"runAsNonRoot: true",
		"runAsUser: 65532",
		"fsGroup: 65532",
		"runAsUser: 0",
		"privileged: true",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered chart missing %q\n%s", want, rendered)
		}
	}
}

func TestHelmCommaSeparatedRegistriesRenderWithSetString(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	out, err := exec.Command("helm", "template", "goodman", ".", "--set-string", `registries=npm\,pypi`).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template with comma registry: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `registries: "npm,pypi"`) {
		t.Fatalf("comma-separated registries were not rendered as one value:\n%s", out)
	}
}

func TestHelmDefaultsUseReleaseImagesAndActionableNotes(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	out, err := exec.Command("helm", "template", "goodman", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	rendered := string(out)
	for _, want := range []string{
		`image: "ghcr.io/goodman-sec/sensor:0.1.0"`,
		`image: "ghcr.io/goodman-sec/collector:0.1.0"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered chart missing %q\n%s", want, rendered)
		}
	}

	notes, err := os.ReadFile("templates/NOTES.txt")
	if err != nil {
		t.Fatalf("read NOTES.txt: %v", err)
	}
	for _, want := range []string{
		`kubectl -n <app-namespace> set env deployment --all NODE_OPTIONS=`,
		`kubectl -n <app-namespace> set env deployment -l app=<app-label> NODE_OPTIONS=`,
	} {
		if !strings.Contains(string(notes), want) {
			t.Fatalf("NOTES.txt missing %q\n%s", want, notes)
		}
	}
}
