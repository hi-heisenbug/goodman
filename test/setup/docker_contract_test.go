package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSensorImageSelectsBPFTargetFromDockerPlatform(t *testing.T) {
	dockerfile, err := os.ReadFile(filepath.Join(setupRepoRoot(t), "deploy", "docker", "sensor.Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(dockerfile)
	for _, want := range []string{"ARG TARGETARCH", "amd64", "arm64", "__TARGET_ARCH_"} {
		if !strings.Contains(text, want) {
			t.Fatalf("sensor image is missing %q multi-arch contract:\n%s", want, text)
		}
	}
	if strings.Contains(text, "-D__TARGET_ARCH_x86 ") {
		t.Fatalf("sensor image hard-codes x86 BPF target:\n%s", text)
	}
}
