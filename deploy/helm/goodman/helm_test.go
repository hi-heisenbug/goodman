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
		`image: "ghcr.io/hi-heisenbug/sensor:0.1.0"`,
		`image: "ghcr.io/hi-heisenbug/collector:0.1.0"`,
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

func TestHelmWebhookOnlyRendersWhenEnabled(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	// Default: no admission webhook objects.
	out, err := exec.Command("helm", "template", "goodman", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "MutatingWebhookConfiguration") {
		t.Fatal("webhook must not render when disabled")
	}

	// Enabled: webhook config, TLS secret, service, and injected flags render.
	out, err = exec.Command("helm", "template", "goodman", ".", "--set", "webhook.enabled=true").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template webhook: %v\n%s", err, out)
	}
	rendered := string(out)
	for _, want := range []string{
		"kind: MutatingWebhookConfiguration",
		"goodman.io/inject: enabled",
		"path: /mutate",
		"-admission-listen=:8443",
		"name: goodman-webhook-tls",
		"caBundle:",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("enabled webhook missing %q\n%s", want, rendered)
		}
	}
}

func TestHelmSQLitePersistencePVC(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	out, err := exec.Command("helm", "template", "goodman", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	rendered := string(out)
	for _, want := range []string{
		"kind: PersistentVolumeClaim",
		"name: goodman-collector-data",
		"claimName: goodman-collector-data",
		"GOODMAN_SPOOL_EVENTS",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("default SQLite persistence missing %q\n%s", want, rendered)
		}
	}

	out, err = exec.Command("helm", "template", "goodman", ".",
		"--set", "store.persistence.enabled=false").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template no-pvc: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "kind: PersistentVolumeClaim") {
		t.Fatal("PVC must not render when store.persistence.enabled=false")
	}
	if !strings.Contains(string(out), "emptyDir: {}") {
		t.Fatal("expected emptyDir when persistence disabled")
	}

	out, err = exec.Command("helm", "template", "goodman", ".",
		"--set", "postgres.dsn=postgres://x").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template postgres: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "kind: PersistentVolumeClaim") {
		t.Fatal("PVC must not render when postgres.dsn is set")
	}
}

func TestHelmHAReplicasRequirePostgres(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	out, err := exec.Command("helm", "template", "goodman", ".",
		"--set", "collector.replicas=2").CombinedOutput()
	if err == nil {
		t.Fatalf("expected helm template to fail without postgres.dsn:\n%s", out)
	}
	if !strings.Contains(string(out), "postgres.dsn") {
		t.Fatalf("expected postgres.dsn error, got:\n%s", out)
	}
}

func TestHelmEnforceDefaultOff(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	out, err := exec.Command("helm", "template", "goodman", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	rendered := string(out)
	if strings.Contains(rendered, "GOODMAN_ENFORCE_ENABLED") {
		t.Fatal("default chart must not set GOODMAN_ENFORCE_ENABLED")
	}
	if strings.Contains(rendered, "name: cgroup") {
		t.Fatal("default chart must not mount host cgroup fs")
	}

	out, err = exec.Command("helm", "template", "goodman", ".", "--set", "enforce.enabled=true").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template enforce: %v\n%s", err, out)
	}
	rendered = string(out)
	for _, want := range []string{
		"GOODMAN_ENFORCE_ENABLED",
		`value: "true"`,
		"name: cgroup",
		"/sys/fs/cgroup",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("enforce.enabled=true missing %q\n%s", want, rendered)
		}
	}
}

func TestHelmHAReplicasWithPostgres(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	out, err := exec.Command("helm", "template", "goodman", ".",
		"--set", "collector.replicas=2",
		"--set", "postgres.dsn=postgres://goodman:secret@db:5432/goodman").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template HA: %v\n%s", err, out)
	}
	rendered := string(out)
	for _, want := range []string{
		"kind: PodDisruptionBudget",
		"minAvailable: 1",
		"GOODMAN_HA_REPLICAS",
		`value: "2"`,
		"podAntiAffinity:",
		"kubernetes.io/hostname",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("HA render missing %q\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "kind: PersistentVolumeClaim") {
		t.Fatal("PVC must not render when replicas>1 even without postgres.dsn guard path")
	}
}
