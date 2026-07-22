// Package loader loads and owns the Goodman eBPF collections and links.
package loader

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

//go:embed goodman.bpf.o
var bpfObject []byte

// WatchedComms are the runtime process names Goodman attaches to by default.

type Options struct {
	ProcRoot string
	Enforce  bool
}

type Loader struct {
	coll        *ebpf.Collection
	enforceColl *ebpf.Collection
	links       []link.Link
	reader      *ringbuf.Reader
	procRoot    string

	watched map[uint32]bool

	enforceRequested bool
	enforceActive    bool
	enforceReason    string
	discarded        atomic.Uint64
	closeOnce        sync.Once
}

// New loads the embedded BPF object and attaches tracepoints.
func New(procRoot string) (*Loader, error) {
	return NewWithOptions(Options{ProcRoot: procRoot})
}

// NewWithOptions loads the BPF object. When Enforce is false, LSM programs are
// stripped before load so detection works on kernels without CONFIG_BPF_LSM.
func NewWithOptions(opt Options) (*Loader, error) {
	if opt.ProcRoot == "" {
		opt.ProcRoot = "/proc"
	}
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("remove memlock rlimit: %w", err)
	}
	baseSpec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(bpfObject))
	if err != nil {
		return nil, fmt.Errorf("parse BPF object: %w", err)
	}

	l := &Loader{
		procRoot:         opt.ProcRoot,
		watched:          map[uint32]bool{},
		enforceRequested: opt.Enforce,
	}

	detectionSpec := baseSpec.Copy()
	deleteEnforcementPrograms(detectionSpec)
	coll, err := ebpf.NewCollection(detectionSpec)
	if err != nil {
		var verr *ebpf.VerifierError
		if errors.As(err, &verr) {
			return nil, fmt.Errorf("BPF verifier rejected program (kernel %s): %+v", kernelRelease(), verr)
		}
		return nil, fmt.Errorf("load BPF collection (need root + kernel>=5.8 with BTF at /sys/kernel/btf/vmlinux): %w", err)
	}
	l.coll = coll

	tracepoints := []struct {
		program  string
		category string
		name     string
		optional bool
	}{
		{program: "trace_open", category: "syscalls", name: "sys_enter_open", optional: true},
		{program: "trace_openat", category: "syscalls", name: "sys_enter_openat"},
		{program: "trace_openat2", category: "syscalls", name: "sys_enter_openat2"},
		{program: "trace_connect", category: "syscalls", name: "sys_enter_connect"},
		{program: "trace_execve", category: "syscalls", name: "sys_enter_execve"},
		{program: "trace_process_fork", category: "sched", name: "sched_process_fork"},
		{program: "trace_process_exit", category: "sched", name: "sched_process_exit"},
	}
	for _, tp := range tracepoints {
		lnk, err := link.Tracepoint(tp.category, tp.name, coll.Programs[tp.program], nil)
		if err != nil {
			if tp.optional && errors.Is(err, os.ErrNotExist) {
				log.Printf("loader: optional tracepoint %s/%s unavailable", tp.category, tp.name)
				continue
			}
			l.Close()
			return nil, fmt.Errorf("attach %s/%s: %w", tp.category, tp.name, err)
		}
		l.links = append(l.links, lnk)
	}

	if opt.Enforce {
		l.tryAttachLSM(baseSpec)
	}

	rd, err := ringbuf.NewReader(coll.Maps["events"])
	if err != nil {
		l.Close()
		return nil, fmt.Errorf("open ring buffer: %w", err)
	}
	l.reader = rd
	return l, nil
}

func kernelRelease() string {
	b, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return strings.TrimSpace(string(b))
}

func (l *Loader) Close() {
	l.closeOnce.Do(func() {
		if l.reader != nil {
			l.reader.Close()
		}
		for _, lnk := range l.links {
			lnk.Close()
		}
		if l.coll != nil {
			l.coll.Close()
		}
		if l.enforceColl != nil {
			l.enforceColl.Close()
		}
	})
}
