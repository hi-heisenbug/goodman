// Package loader loads the Goodman eBPF object, attaches its tracepoints,
// manages the watched-pid map, and streams ring-buffer events.
package loader

import (
	_ "embed"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"

	"github.com/goodman-sec/goodman/internal/model"
)

//go:embed goodman.bpf.o
var bpfObject []byte

// WatchedComms are the runtime process names Goodman attaches to by default.
var WatchedComms = map[string]bool{
	"node": true, "nodejs": true, "python3": true, "python": true,
}

type Loader struct {
	coll     *ebpf.Collection
	links    []link.Link
	reader   *ringbuf.Reader
	procRoot string

	watched map[uint32]bool
}

// New loads the embedded BPF object and attaches the three tracepoints.
// procRoot is "/proc" on a host, "/host/proc" inside the DaemonSet.
func New(procRoot string) (*Loader, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("remove memlock rlimit: %w", err)
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(bpfObject))
	if err != nil {
		return nil, fmt.Errorf("parse BPF object: %w", err)
	}
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		var verr *ebpf.VerifierError
		if errors.As(err, &verr) {
			return nil, fmt.Errorf("BPF verifier rejected program (kernel %s): %+v", kernelRelease(), verr)
		}
		return nil, fmt.Errorf("load BPF collection (need root + kernel>=5.8 with BTF at /sys/kernel/btf/vmlinux): %w", err)
	}

	l := &Loader{coll: coll, procRoot: procRoot, watched: map[uint32]bool{}}
	for prog, tp := range map[string]string{
		"trace_openat":  "sys_enter_openat",
		"trace_connect": "sys_enter_connect",
		"trace_execve":  "sys_enter_execve",
	} {
		lnk, err := link.Tracepoint("syscalls", tp, coll.Programs[prog], nil)
		if err != nil {
			l.Close()
			return nil, fmt.Errorf("attach %s: %w", tp, err)
		}
		l.links = append(l.links, lnk)
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
	if l.reader != nil {
		l.reader.Close()
	}
	for _, lnk := range l.links {
		lnk.Close()
	}
	if l.coll != nil {
		l.coll.Close()
	}
}

// Watch adds a pid to the in-kernel filter.
func (l *Loader) Watch(pid uint32) error {
	one := uint8(1)
	if err := l.coll.Maps["watched_pids"].Put(pid, one); err != nil {
		return err
	}
	l.watched[pid] = true
	return nil
}

func (l *Loader) Unwatch(pid uint32) {
	_ = l.coll.Maps["watched_pids"].Delete(pid)
	delete(l.watched, pid)
}

// Watched returns a snapshot of currently watched pids.
func (l *Loader) Watched() []uint32 {
	out := make([]uint32, 0, len(l.watched))
	for p := range l.watched {
		out = append(out, p)
	}
	return out
}

// Drops returns the kernel-side dropped-event count (ring buffer full).
func (l *Loader) Drops() uint64 {
	var perCPU []uint64
	if err := l.coll.Maps["drops"].Lookup(uint32(0), &perCPU); err != nil {
		return 0
	}
	var total uint64
	for _, v := range perCPU {
		total += v
	}
	return total
}

// RefreshWatched scans procRoot for runtime processes (node/python) and
// syncs the kernel pid filter. onExit is called for pids that disappeared.
func (l *Loader) RefreshWatched(extraComms []string, onNew, onExit func(pid uint32)) error {
	comms := map[string]bool{}
	for c := range WatchedComms {
		comms[c] = true
	}
	for _, c := range extraComms {
		if c != "" {
			comms[c] = true
		}
	}
	entries, err := os.ReadDir(l.procRoot)
	if err != nil {
		return err
	}
	self := uint32(os.Getpid())
	alive := map[uint32]bool{}
	for _, ent := range entries {
		pid64, err := strconv.ParseUint(ent.Name(), 10, 32)
		if err != nil {
			continue
		}
		pid := uint32(pid64)
		if pid == self {
			continue
		}
		commB, err := os.ReadFile(fmt.Sprintf("%s/%d/comm", l.procRoot, pid))
		if err != nil {
			continue
		}
		if !comms[strings.TrimSpace(string(commB))] {
			continue
		}
		alive[pid] = true
		if !l.watched[pid] {
			if err := l.Watch(pid); err != nil {
				log.Printf("loader: watch pid %d: %v", pid, err)
				continue
			}
			if onNew != nil {
				onNew(pid)
			}
		}
	}
	for pid := range l.watched {
		if !alive[pid] {
			l.Unwatch(pid)
			if onExit != nil {
				onExit(pid)
			}
		}
	}
	return nil
}

// Read blocks for the next raw event from the kernel.
func (l *Loader) Read() (*model.RawEvent, error) {
	for {
		rec, err := l.reader.Read()
		if err != nil {
			return nil, err
		}
		if len(rec.RawSample) < model.RawEventSize {
			continue // truncated record; skip rather than misparse
		}
		var ev model.RawEvent
		if err := binary.Read(bytes.NewReader(rec.RawSample[:model.RawEventSize]), binary.LittleEndian, &ev); err != nil {
			continue
		}
		return &ev, nil
	}
}

// BootToUnixNs returns the offset that converts bpf_ktime_get_ns()
// (CLOCK_MONOTONIC since boot) into unix nanoseconds.
func BootToUnixNs() uint64 {
	var mono int64
	f, err := os.ReadFile("/proc/uptime")
	if err == nil {
		fields := strings.Fields(string(f))
		if len(fields) > 0 {
			if up, err := strconv.ParseFloat(fields[0], 64); err == nil {
				mono = int64(up * 1e9)
			}
		}
	}
	return uint64(time.Now().UnixNano() - mono)
}
