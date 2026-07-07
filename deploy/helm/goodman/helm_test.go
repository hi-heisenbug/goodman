package goodman_test

import (
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
