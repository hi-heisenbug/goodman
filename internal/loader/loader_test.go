package loader

import (
	"bytes"
	"testing"
	"unsafe"

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

	for _, prog := range []struct {
		name     string
		attachTo string
	}{
		{"enforce_file_open", "file_open"},
		{"enforce_socket_connect", "socket_connect"},
		{"enforce_bprm_check", "bprm_check_security"},
	} {
		p, ok := spec.Programs[prog.name]
		if !ok {
			t.Errorf("missing LSM program %q", prog.name)
			continue
		}
		if p.Type != ebpf.LSM {
			t.Errorf("program %q type = %v, want LSM", prog.name, p.Type)
		}
		if p.AttachTo != prog.attachTo {
			t.Errorf("program %q attachTo = %q, want %q", prog.name, p.AttachTo, prog.attachTo)
		}
	}

	wantMaps := map[string]ebpf.MapType{
		"events":            ebpf.RingBuf,
		"watched_pids":      ebpf.Hash,
		"drops":             ebpf.PerCPUArray,
		"enforce_deadline":  ebpf.Array,
		"enforced_cgroups":  ebpf.Hash,
		"deny_open":         ebpf.Hash,
		"deny_connect":      ebpf.Hash,
		"deny_exec":         ebpf.Hash,
		"deny_event_drops":  ebpf.PerCPUArray,
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

	if spec.Maps["deny_open"].KeySize != 256 {
		t.Errorf("deny_open key size = %d, want 256", spec.Maps["deny_open"].KeySize)
	}
	if spec.Maps["deny_connect"].KeySize != 20 {
		t.Errorf("deny_connect key size = %d, want 20", spec.Maps["deny_connect"].KeySize)
	}
	if spec.Maps["enforced_cgroups"].KeySize != uint32(unsafe.Sizeof(uint64(0))) {
		t.Errorf("enforced_cgroups key size = %d", spec.Maps["enforced_cgroups"].KeySize)
	}
}
