// Kernel drop counters and userspace discard accounting.
package loader

import (
	"github.com/cilium/ebpf"
)

// Drops returns the kernel-side dropped-event count (ring buffer full).
func (l *Loader) Drops() (uint64, error) {
	return perCPUMapSum(l.coll.Maps["drops"])
}

func (l *Loader) DenyEventDrops() (uint64, error) {
	if l.coll.Maps["deny_event_drops"] == nil {
		return 0, nil
	}
	return perCPUMapSum(l.coll.Maps["deny_event_drops"])
}

func perCPUMapSum(m *ebpf.Map) (uint64, error) {
	var perCPU []uint64
	if err := m.Lookup(uint32(0), &perCPU); err != nil {
		return 0, err
	}
	var total uint64
	for _, v := range perCPU {
		total += v
	}
	return total, nil
}

// Discards returns user-space ring-buffer records rejected because they were
// undersized or could not be decoded. Kernel ring-buffer-full drops are
// reported separately by Drops.
func (l *Loader) Discards() uint64 { return l.discarded.Load() }
