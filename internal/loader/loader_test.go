package loader

import (
	"bytes"
	"testing"
	"unsafe"

	"github.com/cilium/ebpf"

	"github.com/hi-heisenbug/goodman/internal/enforce"
	"github.com/hi-heisenbug/goodman/internal/model"
)

func TestWatchedCommsIncludesCurrentOpenClawGateway(t *testing.T) {
	if !WatchedComms["openclaw-gatewa"] {
		t.Fatal("current OpenClaw Gateway comm openclaw-gatewa must be watched by default")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	loader := &Loader{}
	loader.Close()
	loader.Close()
}

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

	for _, prog := range []string{
		"trace_open", "trace_openat", "trace_openat2", "trace_connect", "trace_execve",
		"trace_process_fork", "trace_process_exit",
	} {
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
		"deny_path_scratch": ebpf.PerCPUArray,
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

	if spec.Maps["deny_open"].KeySize != 264 {
		t.Errorf("deny_open key size = %d, want 264", spec.Maps["deny_open"].KeySize)
	}
	if spec.Maps["deny_connect"].KeySize != 32 {
		t.Errorf("deny_connect key size = %d, want 32", spec.Maps["deny_connect"].KeySize)
	}
	if spec.Maps["enforced_cgroups"].KeySize != uint32(unsafe.Sizeof(uint64(0))) {
		t.Errorf("enforced_cgroups key size = %d", spec.Maps["enforced_cgroups"].KeySize)
	}
}

func TestExpandScopedVerdictsKeepsServicesIsolated(t *testing.T) {
	got := expandScopedVerdicts(map[uint64]string{
		11: "checkout-abc",
		22: "worker-def",
	}, enforce.ServiceVerdicts{
		"checkout-abc": {Open: []string{"/etc/shadow"}},
		"worker-def":   {Exec: []string{"/bin/sh"}},
	})
	if len(got.Open) != 1 || got.Open[0].CgroupID != 11 || got.Open[0].Path != "/etc/shadow" {
		t.Fatalf("open verdicts = %+v", got.Open)
	}
	if len(got.Exec) != 1 || got.Exec[0].CgroupID != 22 || got.Exec[0].Path != "/bin/sh" {
		t.Fatalf("exec verdicts = %+v", got.Exec)
	}
}

func TestDecodeRawEventRejectsShortRecord(t *testing.T) {
	if _, err := decodeRawEvent(make([]byte, model.RawEventSize-1)); err == nil {
		t.Fatal("short ring-buffer record must be rejected")
	}
}
