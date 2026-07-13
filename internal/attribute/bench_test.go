package attribute

import (
	"testing"

	"github.com/hi-heisenbug/goodman/internal/model"
)

// BenchmarkCanonicalize measures the per-event canonicalization cost on the
// sensor hot path (the work done for every captured syscall before batching).
func BenchmarkCanonicalize(b *testing.B) {
	args := []struct {
		t   model.EventType
		arg string
	}{
		{model.EventFileOpen, "/app/node_modules/express/lib/router/route.js"},
		{model.EventFileOpen, "/home/app/.aws/credentials"},
		{model.EventNetConnect, "52.84.12.7:443"},
		{model.EventProcExec, "/usr/bin/curl"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a := args[i%len(args)]
		_ = CanonicalizeWith(a.t, a.arg, 16)
	}
}
