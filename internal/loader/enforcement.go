// Service-scoped deny map reconciliation.
package loader

import (
	"fmt"
	"net"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/hi-heisenbug/goodman/internal/enforce"
	"github.com/hi-heisenbug/goodman/internal/model"
)

// SetEnforceDeadline writes the enforcement kill-switch deadline (CLOCK_MONOTONIC ns).
// Zero disables enforcement immediately.
func (l *Loader) SetEnforceDeadline(ns uint64) error {
	if l.coll.Maps["enforce_deadline"] == nil {
		return nil
	}
	return l.coll.Maps["enforce_deadline"].Put(uint32(0), ns)
}

// ReconcileEnforcement atomically-from-the-policy-perspective replaces the
// service-scoped deny maps. Cgroups are disarmed before any key changes and
// re-armed only after all maps reconcile successfully, so partial updates fail
// open instead of applying another service's verdicts.
func (l *Loader) ReconcileEnforcement(scopes map[uint64]string, services enforce.ServiceVerdicts) error {
	if err := l.reconcileEnforcedCgroups(nil); err != nil {
		return err
	}
	verdicts := expandScopedVerdicts(scopes, services)
	if err := l.reconcileDenyOpen(verdicts.Open); err != nil {
		return err
	}
	if err := l.reconcileDenyConnect(verdicts.Connect); err != nil {
		return err
	}
	if err := l.reconcileDenyExec(verdicts.Exec); err != nil {
		return err
	}
	return l.reconcileEnforcedCgroups(scopes)
}

func (l *Loader) reconcileEnforcedCgroups(scopes map[uint64]string) error {
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
	for id := range scopes {
		if err := m.Put(id, one); err != nil {
			return err
		}
	}
	for _, id := range existing {
		if _, ok := scopes[id]; !ok {
			_ = m.Delete(id)
		}
	}
	return nil
}

type scopedPathVerdict struct {
	CgroupID uint64
	Path     string
}

type scopedConnectVerdict struct {
	CgroupID uint64
	Verdict  enforce.ConnectVerdict
}

type scopedVerdicts struct {
	Open    []scopedPathVerdict
	Connect []scopedConnectVerdict
	Exec    []scopedPathVerdict
}

func expandScopedVerdicts(scopes map[uint64]string, services enforce.ServiceVerdicts) scopedVerdicts {
	var out scopedVerdicts
	for cgroupID, service := range scopes {
		verdicts, ok := services[service]
		if !ok {
			continue
		}
		for _, path := range verdicts.Open {
			out.Open = append(out.Open, scopedPathVerdict{CgroupID: cgroupID, Path: path})
		}
		for _, verdict := range verdicts.Connect {
			out.Connect = append(out.Connect, scopedConnectVerdict{CgroupID: cgroupID, Verdict: verdict})
		}
		for _, path := range verdicts.Exec {
			out.Exec = append(out.Exec, scopedPathVerdict{CgroupID: cgroupID, Path: path})
		}
	}
	return out
}

func (l *Loader) reconcileDenyOpen(verdicts []scopedPathVerdict) error {
	return l.reconcileDenyPath("deny_open", verdicts)
}

func (l *Loader) reconcileDenyExec(verdicts []scopedPathVerdict) error {
	return l.reconcileDenyPath("deny_exec", verdicts)
}

func (l *Loader) reconcileDenyPath(mapName string, verdicts []scopedPathVerdict) error {
	m := l.coll.Maps[mapName]
	if m == nil {
		return nil
	}
	want := map[string]bool{}
	one := uint8(1)
	for _, verdict := range verdicts {
		key := makeDenyPathKey(verdict.CgroupID, verdict.Path)
		want[denyPathKeyString(key)] = true
		if err := m.Put(key, one); err != nil {
			return err
		}
	}
	return deleteMissingDenyPath(m, want)
}

type denyPathKey struct {
	CgroupID uint64
	Path     [model.PathMaxLen]byte
}

func makeDenyPathKey(cgroupID uint64, path string) denyPathKey {
	k := denyPathKey{CgroupID: cgroupID}
	copy(k.Path[:], path)
	return k
}

func denyPathKeyString(k denyPathKey) string {
	return fmt.Sprintf("%d:%s", k.CgroupID, string(k.Path[:]))
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
		if !want[denyPathKeyString(k)] {
			_ = m.Delete(k)
		}
	}
	return nil
}

type denyAddrKey struct {
	CgroupID uint64
	Family   uint8
	Pad      uint8
	Port     uint16
	Addr     [16]byte
}

func (l *Loader) reconcileDenyConnect(entries []scopedConnectVerdict) error {
	m := l.coll.Maps["deny_connect"]
	if m == nil {
		return nil
	}
	want := map[string]bool{}
	one := uint8(1)
	for _, e := range entries {
		key, err := denyAddrKeyFromVerdict(e.CgroupID, e.Verdict)
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

func denyAddrKeyFromVerdict(cgroupID uint64, v enforce.ConnectVerdict) (denyAddrKey, error) {
	ip := net.ParseIP(v.Addr)
	if ip == nil {
		return denyAddrKey{}, fmt.Errorf("bad ip")
	}
	k := denyAddrKey{CgroupID: cgroupID}
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
	return fmt.Sprintf("%d:%d:%d:%x", k.CgroupID, k.Family, k.Port, k.Addr)
}

// RefreshWatched scans procRoot for runtime processes and syncs the kernel pid
// filter. onExit is called for pids that disappeared.

func init() {
	// Ensure deny key sizes match BPF expectations.
	if unsafe.Sizeof(denyPathKey{}) != 8+model.PathMaxLen {
		panic("denyPathKey size mismatch")
	}
	if unsafe.Sizeof(denyAddrKey{}) != 32 {
		panic("denyAddrKey size mismatch")
	}
}
