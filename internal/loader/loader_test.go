package loader

import (
	"bytes"
	"testing"

	"github.com/cilium/ebpf"
)

// TestEmbeddedObjectSpec parses the embedded eBPF object (no privileges
// required) and asserts the tracepoint programs and the maps the loader
// relies on are present with the expected types. This catches a stale/broken
// .o without needing root to actually load it into the kernel.
func TestEmbeddedObjectSpec(t *testing.T) {
	if len(bpfObject) == 0 {
		t.Fatal("embedded goodman.bpf.o is empty — run `make bpf`")
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(bpfObject))
	if err != nil {
		t.Fatalf("parse embedded object: %v", err)
	}

	for _, prog := range []string{"trace_open", "trace_openat", "trace_openat2", "trace_connect", "trace_execve"} {
		p, ok := spec.Programs[prog]
		if !ok {
			t.Errorf("missing program %q", prog)
			continue
		}
		if p.Type != ebpf.TracePoint {
			t.Errorf("program %q type = %v, want TracePoint", prog, p.Type)
		}
	}

	wantMaps := map[string]ebpf.MapType{
		"events":       ebpf.RingBuf,
		"watched_pids": ebpf.Hash,
		"drops":        ebpf.PerCPUArray,
	}
	for name, typ := range wantMaps {
		m, ok := spec.Maps[name]
		if !ok {
			t.Errorf("missing map %q", name)
			continue
		}
		if m.Type != typ {
			t.Errorf("map %q type = %v, want %v", name, m.Type, typ)
		}
	}
}
