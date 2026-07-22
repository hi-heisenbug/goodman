// Runtime process discovery and watched-pid map management.
package loader

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// WatchedComms are the runtime process names Goodman attaches to by default.
var WatchedComms = map[string]bool{
	"node": true, "nodejs": true, "MainThread": true,
	// OpenClaw sets process.title="openclaw-gateway". Linux TASK_COMM_LEN is
	// 16 bytes including NUL, so /proc/<pid>/comm exposes this exact truncation.
	"openclaw-gatewa": true,
	"python3":         true, "python": true,
	"python3.12": true, "python3.13": true,
	"gunicorn": true, "celery": true, "uwsgi": true, "uvicorn": true,
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

// RefreshWatched scans procRoot for runtime processes and syncs the kernel pid
// filter. onExit is called for pids that disappeared.
func (l *Loader) RefreshWatched(extraComms []string, onNew, onExit func(pid uint32)) error {
	alive, err := l.runtimePIDs(watchedComms(extraComms))
	if err != nil {
		return err
	}
	l.syncWatched(alive, onNew, onExit)
	return nil
}

func watchedComms(extra []string) map[string]bool {
	comms := make(map[string]bool, len(WatchedComms)+len(extra))
	for comm := range WatchedComms {
		comms[comm] = true
	}
	for _, comm := range extra {
		if comm != "" {
			comms[comm] = true
		}
	}
	return comms
}

func (l *Loader) runtimePIDs(comms map[string]bool) (map[uint32]bool, error) {
	entries, err := os.ReadDir(l.procRoot)
	if err != nil {
		return nil, err
	}
	self := uint32(os.Getpid())
	alive := map[uint32]bool{}
	for _, ent := range entries {
		pid, comm, ok := l.runtimeProcess(ent.Name())
		if !ok || pid == self || !comms[comm] {
			continue
		}
		alive[pid] = true
	}
	return alive, nil
}

func (l *Loader) runtimeProcess(name string) (uint32, string, bool) {
	pid64, err := strconv.ParseUint(name, 10, 32)
	if err != nil {
		return 0, "", false
	}
	pid := uint32(pid64)
	comm, err := os.ReadFile(fmt.Sprintf("%s/%d/comm", l.procRoot, pid))
	if err != nil {
		return 0, "", false
	}
	return pid, strings.TrimSpace(string(comm)), true
}

func (l *Loader) syncWatched(alive map[uint32]bool, onNew, onExit func(pid uint32)) {
	for pid := range alive {
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
}
