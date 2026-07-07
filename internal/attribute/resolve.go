package attribute

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/goodman-sec/goodman/internal/model"
)

// Resolver turns raw kernel events (pid + stack of instruction pointers)
// into attributed events (package@version + canonical behavior).
type Resolver struct {
	procRoot string // "/proc" locally, "/host/proc" in k8s

	mu   sync.Mutex
	pids map[int]*pidState
}

type pidState struct {
	perf    *PerfMap
	maps    *ProcMaps
	pidRoot string
	service string
}

func NewResolver(procRoot string) *Resolver {
	return &Resolver{procRoot: procRoot, pids: map[int]*pidState{}}
}

func (r *Resolver) state(pid int) *pidState {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.pids[pid]
	if !ok {
		nspid := NSPID(r.procRoot, pid)
		st = &pidState{
			perf:    NewPerfMap(PerfMapPath(r.procRoot, pid, nspid)),
			maps:    NewProcMaps(r.procRoot, pid),
			pidRoot: fmt.Sprintf("%s/%d/root", r.procRoot, pid),
			service: detectService(r.procRoot, pid),
		}
		r.pids[pid] = st
	}
	return st
}

// Forget drops cached state for an exited pid.
func (r *Resolver) Forget(pid int) {
	r.mu.Lock()
	delete(r.pids, pid)
	r.mu.Unlock()
}

// perf map symbols embed the JS source location, e.g.
// "LazyCompile:*handleRequest /app/node_modules/pkg/dist/router.js:412:19"
var jsPathRe = regexp.MustCompile(`\s(\/[^\s]+?\.[cm]?js)(?::\d+)?(?::\d+)?$`)

// sourcePathOf extracts the JS source file path from a perf-map symbol name.
func sourcePathOf(sym string) (string, bool) {
	m := jsPathRe.FindStringSubmatch(sym)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// Frame is one resolved stack frame (for goodmanctl attribute --pid output).
type Frame struct {
	Addr   uint64
	Symbol string // JIT symbol or native module path; "" if unresolved
	Source string // JS source file path if known
}

// ResolveStack resolves every address in the stack for debugging/inspection.
func (r *Resolver) ResolveStack(pid int, stack []uint64) []Frame {
	st := r.state(pid)
	frames := make([]Frame, 0, len(stack))
	for _, addr := range stack {
		f := Frame{Addr: addr}
		if sym, ok := st.perf.Lookup(addr); ok {
			f.Symbol = sym
			if src, ok := sourcePathOf(sym); ok {
				f.Source = src
			}
		} else if m, ok := st.maps.Lookup(addr); ok && m.Path != "" {
			f.Symbol = filepath.Base(m.Path)
			// Native addons live in node_modules too: .../node_modules/x/build/Release/x.node
			if strings.Contains(m.Path, "/node_modules/") {
				f.Source = m.Path
			}
		}
		frames = append(frames, f)
	}
	return frames
}

// Attribute resolves one raw event into (service, package, version, behavior).
// It never misattributes: when no frame resolves into node_modules the event
// is attributed to the application itself, and when even the app source is
// unknown, Package is "<unknown>" — honest over wrong.
func (r *Resolver) Attribute(ev *model.RawEvent, bootToUnixNs uint64) model.Attributed {
	pid := int(ev.PID)
	st := r.state(pid)

	pkg, version := "", ""
	appSource := ""
	// stack[0] is the innermost (deepest) frame. The first frame that
	// resolves into a node_modules path is the deepest actor.
	for _, addr := range ev.UserStack() {
		var src string
		if sym, ok := st.perf.Lookup(addr); ok {
			s, ok := sourcePathOf(sym)
			if !ok {
				continue // builtin / stub / RegExp — no source location
			}
			src = s
		} else if m, ok := st.maps.Lookup(addr); ok && strings.Contains(m.Path, "/node_modules/") {
			src = m.Path // native addon inside node_modules
		} else {
			continue
		}
		if strings.Contains(src, "/node_modules/") {
			if p, v, ok := PathToPackage(st.pidRoot, src); ok {
				pkg, version = p, v
			} else {
				// node_modules frame but unreadable package.json: attribute
				// the package name without version rather than guessing.
				if p, _, _ := splitNodeModules(src); p != "" {
					pkg, version = p, ""
				}
			}
			break
		}
		if appSource == "" && !strings.HasPrefix(src, "node:") {
			appSource = src
		}
	}
	if pkg == "" {
		if appSource != "" {
			pkg = "<app>" // application's own code, not a dependency
			version = appVersion(st.pidRoot, appSource)
		} else {
			pkg = "<unknown>"
		}
	}

	return model.Attributed{
		Service:   st.service,
		Package:   pkg,
		Version:   version,
		Type:      model.EventType(ev.Type),
		Behavior:  Canonicalize(model.EventType(ev.Type), ev.ArgString()),
		Timestamp: ev.Timestamp + bootToUnixNs,
	}
}

func splitNodeModules(path string) (pkg, dir, rest string) {
	idx := strings.LastIndex(path, "/node_modules/")
	if idx == -1 {
		return "", "", ""
	}
	tail := path[idx+len("/node_modules/"):]
	parts := strings.SplitN(tail, "/", 3)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", ""
	}
	if strings.HasPrefix(parts[0], "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1], path[:idx], tail
	}
	return parts[0], path[:idx], tail
}

// appVersion finds the application's own package.json version by walking up
// from the app source file.
func appVersion(pidRoot, srcPath string) string {
	dir := filepath.Dir(srcPath)
	for i := 0; i < 8 && dir != "/" && dir != "."; i++ {
		v := readVersionField(filepath.Join(pidRoot, dir, "package.json"))
		if v != "" {
			return v
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

// detectService determines the service name for a pid: the k8s pod name via
// the HOSTNAME env var when running under a kubepods cgroup, else the
// process's working directory basename (local dev).
func detectService(procRoot string, pid int) string {
	cg, _ := os.ReadFile(fmt.Sprintf("%s/%d/cgroup", procRoot, pid))
	if strings.Contains(string(cg), "kubepods") {
		if env, err := os.ReadFile(fmt.Sprintf("%s/%d/environ", procRoot, pid)); err == nil {
			for _, kv := range strings.Split(string(env), "\x00") {
				if v, ok := strings.CutPrefix(kv, "HOSTNAME="); ok && v != "" {
					return v
				}
			}
		}
	}
	if cwd, err := os.Readlink(fmt.Sprintf("%s/%d/cwd", procRoot, pid)); err == nil {
		if b := filepath.Base(cwd); b != "/" && b != "." {
			return b
		}
	}
	return fmt.Sprintf("pid-%d", pid)
}
