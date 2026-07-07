package attribute

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

// Mapping is one executable region from /proc/<pid>/maps.
type Mapping struct {
	Start, End uint64
	Path       string // backing file ("" for anonymous, e.g. JIT regions)
}

// ProcMaps caches the executable mappings of one process.
type ProcMaps struct {
	mu       sync.Mutex
	procRoot string
	pid      int
	loaded   time.Time
	regions  []Mapping // sorted by Start
}

func NewProcMaps(procRoot string, pid int) *ProcMaps {
	return &ProcMaps{procRoot: procRoot, pid: pid}
}

const mapsTTL = 5 * time.Second

func (m *ProcMaps) refresh() error {
	if time.Since(m.loaded) < mapsTTL && m.regions != nil {
		return nil
	}
	f, err := os.Open(fmt.Sprintf("%s/%d/maps", m.procRoot, m.pid))
	if err != nil {
		return err
	}
	defer f.Close()

	var regions []Mapping
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		// 7f2a3c000000-7f2a3c021000 r-xp 00000000 08:01 131342  /usr/lib/x86_64-linux-gnu/libc.so.6
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || !strings.Contains(fields[1], "x") {
			continue
		}
		dash := strings.IndexByte(fields[0], '-')
		if dash < 0 {
			continue
		}
		start, err1 := strconv.ParseUint(fields[0][:dash], 16, 64)
		end, err2 := strconv.ParseUint(fields[0][dash+1:], 16, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		path := ""
		if len(fields) >= 6 {
			path = strings.Join(fields[5:], " ")
		}
		regions = append(regions, Mapping{Start: start, End: end, Path: path})
	}
	if err := sc.Err(); err != nil {
		return err
	}
	sort.Slice(regions, func(i, j int) bool { return regions[i].Start < regions[j].Start })
	m.regions = regions
	m.loaded = time.Now()
	return nil
}

// Lookup returns the executable mapping containing addr, if any.
func (m *ProcMaps) Lookup(addr uint64) (Mapping, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.refresh(); err != nil {
		return Mapping{}, false
	}
	i := sort.Search(len(m.regions), func(i int) bool { return m.regions[i].End > addr })
	if i < len(m.regions) && m.regions[i].Start <= addr {
		return m.regions[i], true
	}
	return Mapping{}, false
}
