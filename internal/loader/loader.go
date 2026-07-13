// Package loader loads the Goodman eBPF object, attaches its tracepoints,
// manages the watched-pid map, and streams ring-buffer events.
package loader

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/features"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"

	"github.com/hi-heisenbug/goodman/internal/enforce"
	"github.com/hi-heisenbug/goodman/internal/model"
)

//go:embed goodman.bpf.o
var bpfObject []byte

// WatchedComms are the runtime process names Goodman attaches to by default.
var WatchedComms = map[string]bool{
	"node": true, "nodejs": true, "MainThread": true,
	"python3": true, "python": true,
	"python3.12": true, "python3.13": true,
	"gunicorn": true, "celery": true, "uwsgi": true, "uvicorn": true,
}

type Options struct {
	ProcRoot string
	Enforce  bool
}

type Loader struct {
	coll     *ebpf.Collection
	links    []link.Link
	reader   *ringbuf.Reader
	procRoot string

	watched map[uint32]bool

	enforceRequested bool
	enforceActive    bool
	enforceReason    string
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
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(bpfObject))
	if err != nil {
		return nil, fmt.Errorf("parse BPF object: %w", err)
	}

	l := &Loader{
		procRoot:         opt.ProcRoot,
		watched:          map[uint32]bool{},
		enforceRequested: opt.Enforce,
	}

	if !opt.Enforce {
		delete(spec.Programs, "enforce_file_open")
		delete(spec.Programs, "enforce_socket_connect")
		delete(spec.Programs, "enforce_bprm_check")
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		var verr *ebpf.VerifierError
		if errors.As(err, &verr) {
			return nil, fmt.Errorf("BPF verifier rejected program (kernel %s): %+v", kernelRelease(), verr)
		}
		return nil, fmt.Errorf("load BPF collection (need root + kernel>=5.8 with BTF at /sys/kernel/btf/vmlinux): %w", err)
	}
	l.coll = coll

	for prog, tp := range map[string]string{
		"trace_open":    "sys_enter_open",
		"trace_openat":  "sys_enter_openat",
		"trace_openat2": "sys_enter_openat2",
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

	if opt.Enforce {
		l.tryAttachLSM(coll)
	}

	rd, err := ringbuf.NewReader(coll.Maps["events"])
	if err != nil {
		l.Close()
		return nil, fmt.Errorf("open ring buffer: %w", err)
	}
	l.reader = rd
	return l, nil
}

func (l *Loader) tryAttachLSM(coll *ebpf.Collection) {
	reason := lsmSupportReason()
	if reason != "" {
		l.enforceReason = reason
		log.Printf("loader: LSM enforcement unavailable (%s); detection-only", reason)
		return
	}
	for _, name := range []string{"enforce_file_open", "enforce_socket_connect", "enforce_bprm_check"} {
		prog := coll.Programs[name]
		if prog == nil {
			l.enforceReason = "LSM program missing from collection"
			return
		}
		lnk, err := link.AttachLSM(link.LSMOptions{Program: prog})
		if err != nil {
			l.enforceReason = fmt.Sprintf("attach %s: %v", name, err)
			for _, existing := range l.links {
				if existing != nil {
					_ = existing.Close()
				}
			}
			l.links = nil
			log.Printf("loader: LSM attach failed (%s); detection-only", l.enforceReason)
			return
		}
		l.links = append(l.links, lnk)
	}
	l.enforceActive = true
	l.enforceReason = "active"
	log.Printf("loader: LSM enforcement programs attached")
}

func lsmSupportReason() string {
	if err := features.HaveProgramType(ebpf.LSM); err != nil {
		return err.Error()
	}
	b, err := os.ReadFile("/sys/kernel/security/lsm")
	if err != nil {
		return "cannot read /sys/kernel/security/lsm"
	}
	if !strings.Contains(string(b), "bpf") {
		return `active lsm= list does not include "bpf"`
	}
	return ""
}

func (l *Loader) EnforcementActive() bool { return l.enforceActive }
func (l *Loader) EnforcementReason() string {
	if l.enforceReason == "" {
		if l.enforceRequested {
			return "not probed"
		}
		return "disabled"
	}
	return l.enforceReason
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
	return perCPUMapSum(l.coll.Maps["drops"])
}

func (l *Loader) DenyEventDrops() uint64 {
	if l.coll.Maps["deny_event_drops"] == nil {
		return 0
	}
	return perCPUMapSum(l.coll.Maps["deny_event_drops"])
}

func perCPUMapSum(m *ebpf.Map) uint64 {
	var perCPU []uint64
	if err := m.Lookup(uint32(0), &perCPU); err != nil {
		return 0
	}
	var total uint64
	for _, v := range perCPU {
		total += v
	}
	return total
}

// SetEnforceDeadline writes the enforcement kill-switch deadline (CLOCK_MONOTONIC ns).
// Zero disables enforcement immediately.
func (l *Loader) SetEnforceDeadline(ns uint64) error {
	if l.coll.Maps["enforce_deadline"] == nil {
		return nil
	}
	return l.coll.Maps["enforce_deadline"].Put(uint32(0), ns)
}

// ReconcileEnforcedCgroups syncs the enforced cgroup scope map.
func (l *Loader) ReconcileEnforcedCgroups(ids map[uint64]bool) error {
	m := l.coll.Maps["enforced_cgroups"]
	if m == nil {
		return nil
	}
	var existing []uint64
	iter := m.Iterate()
	var k uint64
	var v uint8
	for iter.Next(&k, &v) {
		existing = append(existing, k)
	}
	one := uint8(1)
	for id := range ids {
		if err := m.Put(id, one); err != nil {
			return err
		}
	}
	for _, id := range existing {
		if !ids[id] {
			_ = m.Delete(id)
		}
	}
	return nil
}

// ReconcileDenyMaps syncs literal deny verdict maps from user space.
func (l *Loader) ReconcileDenyMaps(vs enforce.VerdictSet) error {
	if err := l.reconcileDenyOpen(vs.Open); err != nil {
		return err
	}
	if err := l.reconcileDenyConnect(vs.Connect); err != nil {
		return err
	}
	return l.reconcileDenyExec(vs.Exec)
}

func (l *Loader) reconcileDenyOpen(paths []string) error {
	m := l.coll.Maps["deny_open"]
	if m == nil {
		return nil
	}
	want := map[string]bool{}
	one := uint8(1)
	for _, p := range paths {
		key := makeDenyPathKey(p)
		want[string(key[:])] = true
		if err := m.Put(key, one); err != nil {
			return err
		}
	}
	return deleteMissingDenyPath(m, want)
}

func (l *Loader) reconcileDenyExec(paths []string) error {
	m := l.coll.Maps["deny_exec"]
	if m == nil {
		return nil
	}
	want := map[string]bool{}
	one := uint8(1)
	for _, p := range paths {
		key := makeDenyPathKey(p)
		want[string(key[:])] = true
		if err := m.Put(key, one); err != nil {
			return err
		}
	}
	return deleteMissingDenyPath(m, want)
}

type denyPathKey [model.PathMaxLen]byte

func makeDenyPathKey(path string) denyPathKey {
	var k denyPathKey
	copy(k[:], path)
	return k
}

func deleteMissingDenyPath(m *ebpf.Map, want map[string]bool) error {
	var existing []denyPathKey
	iter := m.Iterate()
	var k denyPathKey
	var v uint8
	for iter.Next(&k, &v) {
		existing = append(existing, k)
	}
	for _, k := range existing {
		if !want[string(k[:])] {
			_ = m.Delete(k)
		}
	}
	return nil
}

type denyAddrKey struct {
	Family uint8
	Pad    uint8
	Port   uint16
	Addr   [16]byte
}

func (l *Loader) reconcileDenyConnect(entries []enforce.ConnectVerdict) error {
	m := l.coll.Maps["deny_connect"]
	if m == nil {
		return nil
	}
	want := map[string]bool{}
	one := uint8(1)
	for _, e := range entries {
		key, err := denyAddrKeyFromVerdict(e)
		if err != nil {
			continue
		}
		want[denyAddrKeyString(key)] = true
		if err := m.Put(key, one); err != nil {
			return err
		}
	}
	var existing []denyAddrKey
	iter := m.Iterate()
	var k denyAddrKey
	var v uint8
	for iter.Next(&k, &v) {
		existing = append(existing, k)
	}
	for _, k := range existing {
		if !want[denyAddrKeyString(k)] {
			_ = m.Delete(k)
		}
	}
	return nil
}

func denyAddrKeyFromVerdict(v enforce.ConnectVerdict) (denyAddrKey, error) {
	ip := net.ParseIP(v.Addr)
	if ip == nil {
		return denyAddrKey{}, fmt.Errorf("bad ip")
	}
	var k denyAddrKey
	k.Port = v.Port
	if v4 := ip.To4(); v4 != nil {
		k.Family = 2 // AF_INET
		copy(k.Addr[:4], v4)
		return k, nil
	}
	k.Family = 10 // AF_INET6
	copy(k.Addr[:], ip.To16())
	return k, nil
}

func denyAddrKeyString(k denyAddrKey) string {
	return fmt.Sprintf("%d:%d:%x", k.Family, k.Port, k.Addr)
}

// RefreshWatched scans procRoot for runtime processes and syncs the kernel pid
// filter. onExit is called for pids that disappeared.
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
			continue
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

// MonotonicNowNs returns CLOCK_MONOTONIC nanoseconds (matches bpf_ktime_get_ns).
func MonotonicNowNs() uint64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return uint64(time.Now().UnixNano())
	}
	return uint64(ts.Sec)*1e9 + uint64(ts.Nsec)
}

func init() {
	// Ensure deny key sizes match BPF expectations.
	if unsafe.Sizeof(denyPathKey{}) != model.PathMaxLen {
		panic("denyPathKey size mismatch")
	}
}
