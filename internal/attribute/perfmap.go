package attribute

// Tier 1 JIT resolution: when Node runs with
// --perf-basic-prof-only-functions --interpreted-frames-native-stack, V8 appends lines to
// /tmp/perf-<pid>.map:
//
//   3ca9f8c04a20 1e0 LazyCompile:*handleRequest /app/node_modules/pkg/dist/router.js:412:19
//
// We load that file into a sorted interval list and binary-search stack
// addresses into symbols.

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type jitSymbol struct {
	Start uint64
	Size  uint64
	Name  string
}

// PerfMap resolves JIT addresses for one process via its perf map file.
type PerfMap struct {
	mu      sync.Mutex
	path    string
	mtime   time.Time
	size    int64
	checked time.Time
	syms    []jitSymbol // sorted by Start; later duplicates win (V8 re-JITs)
}

// PerfMapPath returns the perf map path for a target process as seen from
// this process. procRoot is "/proc" locally or "/host/proc" in k8s.
// nsPID is the pid in the target's own namespace (V8 names the file after
// the pid it sees); pid is the host-view pid used for /proc access.
func PerfMapPath(procRoot string, pid, nsPID int) string {
	// /proc/<pid>/root/tmp/... always sees the target's own /tmp regardless
	// of mount namespaces — works both locally and inside containers.
	return fmt.Sprintf("%s/%d/root/tmp/perf-%d.map", procRoot, pid, nsPID)
}

// NSPID reads the innermost namespace pid of pid from /proc/<pid>/status.
// Falls back to pid itself when NSpid is absent (no pid namespace).
func NSPID(procRoot string, pid int) int {
	f, err := os.Open(fmt.Sprintf("%s/%d/status", procRoot, pid))
	if err != nil {
		return pid
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "NSpid:") {
			fields := strings.Fields(line[len("NSpid:"):])
			if len(fields) > 0 {
				if v, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
					return v
				}
			}
		}
	}
	return pid
}

func NewPerfMap(path string) *PerfMap {
	return &PerfMap{path: path}
}

const perfMapCheckEvery = 2 * time.Second

// refresh re-parses the file if its mtime or size changed (V8 appends
// continuously while the process runs).
func (p *PerfMap) refresh() error {
	if time.Since(p.checked) < perfMapCheckEvery && p.syms != nil {
		return nil
	}
	p.checked = time.Now()
	st, err := os.Stat(p.path)
	if err != nil {
		return err
	}
	if st.ModTime().Equal(p.mtime) && st.Size() == p.size && p.syms != nil {
		return nil
	}
	f, err := os.Open(p.path)
	if err != nil {
		return err
	}
	defer f.Close()

	var syms []jitSymbol
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		// <hex_start> <hex_size> <symbol...>
		sp1 := strings.IndexByte(line, ' ')
		if sp1 <= 0 {
			continue
		}
		sp2 := strings.IndexByte(line[sp1+1:], ' ')
		if sp2 <= 0 {
			continue
		}
		sp2 += sp1 + 1
		start, err1 := strconv.ParseUint(line[:sp1], 16, 64)
		size, err2 := strconv.ParseUint(line[sp1+1:sp2], 16, 64)
		if err1 != nil || err2 != nil || size == 0 {
			continue
		}
		syms = append(syms, jitSymbol{Start: start, Size: size, Name: line[sp2+1:]})
	}
	if err := sc.Err(); err != nil {
		return err
	}
	// Sort by start; stable so that for identical starts the LATER line
	// (more recent JIT) stays after — Lookup picks the last match below.
	sort.SliceStable(syms, func(i, j int) bool { return syms[i].Start < syms[j].Start })
	p.syms = syms
	p.mtime = st.ModTime()
	p.size = st.Size()
	return nil
}

// Lookup resolves addr to a JIT symbol name.
func (p *PerfMap) Lookup(addr uint64) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.refresh(); err != nil {
		return "", false
	}
	// First symbol with Start > addr; candidates are before i. V8 reuses
	// code ranges, so scan backwards for the most recent covering symbol.
	i := sort.Search(len(p.syms), func(i int) bool { return p.syms[i].Start > addr })
	for j := i - 1; j >= 0 && j >= i-16; j-- {
		s := p.syms[j]
		if addr >= s.Start && addr < s.Start+s.Size {
			return s.Name, true
		}
		if s.Start+s.Size < addr && j < i-1 {
			// Regions are mostly non-overlapping; stop once clearly past.
			break
		}
	}
	return "", false
}
