package attribute

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
)

// Resolver turns raw kernel events (pid + stack of instruction pointers)
// into attributed events (package@version + canonical behavior).
type Resolver struct {
	procRoot string // "/proc" locally, "/host/proc" in k8s

	mu   sync.Mutex
	pids map[int]*pidState

	// ConnectCIDRBits, when in [8,32], aggregates public destination IPs to
	// that IPv4 prefix in CONNECT behaviors (0 = exact IPs, the default).
	ConnectCIDRBits int
}

type pidState struct {
	perf          *PerfMap
	maps          *ProcMaps
	pidRoot       string
	service       string
	threadContext map[uint32]packageContext
}

type packageContext struct {
	pkg       string
	version   string
	timestamp uint64
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
			perf:          NewPerfMap(PerfMapPath(r.procRoot, pid, nspid)),
			maps:          NewProcMaps(r.procRoot, pid),
			pidRoot:       fmt.Sprintf("%s/%d/root", r.procRoot, pid),
			service:       detectService(r.procRoot, pid),
			threadContext: map[uint32]packageContext{},
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
	// Package and ClawHub metadata caches include /proc/<pid>/root paths. Clear
	// them before Linux can reuse the pid for a different filesystem/process.
	FlushVersionCache()
}

// perf map symbols embed the JS source location, e.g.
// "LazyCompile:*handleRequest /app/node_modules/pkg/dist/router.js:412:19"
var jsPathRe = regexp.MustCompile(`\s(\/[^\s]+?\.[cm]?js)(?::\d+)?(?::\d+)?$`)

// CPython 3.12+ perf trampoline: "py::<qualname>:<filename>".
// Accept only absolute .py paths; "<frozen importlib._bootstrap>" and
// friends must never become a package (never-misattribute).
var pySymRe = regexp.MustCompile(`^py::.+:(/[^\s]+\.py)$`)

// sourcePathOf extracts a source file path from a perf-map symbol name.
// JS is tried first (unchanged); then CPython py:: symbols.
func sourcePathOf(sym string) (string, bool) {
	if m := jsPathRe.FindStringSubmatch(sym); m != nil {
		return m[1], true
	}
	if strings.HasPrefix(sym, "py::") {
		if m := pySymRe.FindStringSubmatch(sym); m != nil {
			return m[1], true
		}
	}
	return "", false
}

const packageContextTTL = 250 * time.Millisecond
const maxThreadContexts = 4096

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
			if strings.Contains(m.Path, "/node_modules/") ||
				strings.Contains(m.Path, "/site-packages/") ||
				strings.Contains(m.Path, "/dist-packages/") {
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
	eventType := model.EventType(ev.Type)
	packagePath, behaviorArg := resolveEventPaths(r.procRoot, pid, ev, eventType)
	pkg, version, appSource := resolveStackIdentity(st, ev.UserStack())
	refreshContext := pkg != ""
	if pkg == "" {
		pkg, version, refreshContext = fallbackIdentity(st, ev, eventType, packagePath, appSource)
	}
	if refreshContext && isDependencyIdentity(ev.TID, pkg) {
		rememberPackageContext(st, ev.TID, packageContext{pkg: pkg, version: version, timestamp: ev.Timestamp})
	}

	return model.Attributed{
		Service:   st.service,
		Package:   pkg,
		Version:   version,
		Type:      eventType,
		Behavior:  CanonicalizeWith(eventType, behaviorArg, r.ConnectCIDRBits),
		Timestamp: ev.Timestamp + bootToUnixNs,
	}
}

func resolveEventPaths(procRoot string, pid int, ev *model.RawEvent, eventType model.EventType) (string, string) {
	raw := ev.ArgString()
	if eventType != model.EventFileOpen && eventType != model.EventProcExec {
		return raw, raw
	}
	resolved, ok := resolveProcessPath(procRoot, pid, ev.DirFD, raw)
	if ok {
		return resolved, resolved
	}
	return raw, unresolvedPath(resolved)
}

func resolveStackIdentity(st *pidState, stack []uint64) (string, string, string) {
	appSource := ""
	// stack[0] is the innermost frame, so the first dependency source is the
	// deepest actor and the only safe package identity for this event.
	for _, addr := range stack {
		source, ok := frameSource(st, addr)
		if !ok {
			continue
		}
		if pkg, version, ok := packageFromSource(st.pidRoot, source); ok {
			return pkg, version, appSource
		}
		if appSource == "" && !strings.HasPrefix(source, "node:") {
			appSource = source
		}
	}
	return "", "", appSource
}

func frameSource(st *pidState, addr uint64) (string, bool) {
	if symbol, ok := st.perf.Lookup(addr); ok {
		return sourcePathOf(symbol)
	}
	mapping, ok := st.maps.Lookup(addr)
	if !ok || !isDependencyPath(mapping.Path) {
		return "", false
	}
	return mapping.Path, true
}

func packageFromSource(pidRoot, source string) (string, string, bool) {
	if strings.Contains(source, "/node_modules/") {
		if pkg, version, ok := PathToPackage(pidRoot, source); ok {
			return pkg, version, true
		}
		pkg, _, _ := splitNodeModules(source)
		return pkg, "", true
	}
	if strings.Contains(source, "/site-packages/") || strings.Contains(source, "/dist-packages/") {
		if pkg, version, ok := PathToPyPackage(pidRoot, source); ok {
			return pkg, version, true
		}
		pkg, _, _, _ := splitSitePackages(source)
		return pkg, "", true
	}
	return PathToOpenClawSkill(pidRoot, source)
}

func isDependencyPath(path string) bool {
	return strings.Contains(path, "/node_modules/") ||
		strings.Contains(path, "/site-packages/") ||
		strings.Contains(path, "/dist-packages/")
}

func fallbackIdentity(st *pidState, ev *model.RawEvent, eventType model.EventType, packagePath, appSource string) (string, string, bool) {
	if pkg, version, ok := packageFromOpenedPath(st.pidRoot, eventType, packagePath); ok {
		return pkg, version, true
	}
	if appSource != "" {
		return "<app>", appVersion(st.pidRoot, appSource), false
	}
	if context, ok := recentPackageContext(st, ev); ok {
		return context.pkg, context.version, false
	}
	return "<unknown>", "", false
}

func recentPackageContext(st *pidState, ev *model.RawEvent) (packageContext, bool) {
	if ev.TID == 0 || !contextEligible(ev) {
		return packageContext{}, false
	}
	context, ok := st.threadContext[ev.TID]
	if !ok || ev.Timestamp < context.timestamp || ev.Timestamp-context.timestamp > uint64(packageContextTTL) {
		return packageContext{}, false
	}
	return context, true
}

func isDependencyIdentity(tid uint32, pkg string) bool {
	return tid != 0 && pkg != "" && pkg != "<app>" && pkg != "<unknown>"
}

func rememberPackageContext(st *pidState, tid uint32, next packageContext) {
	if len(st.threadContext) >= maxThreadContexts {
		ttl := uint64(packageContextTTL)
		for existingTID, existing := range st.threadContext {
			if next.timestamp >= existing.timestamp && next.timestamp-existing.timestamp > ttl {
				delete(st.threadContext, existingTID)
			}
		}
		if len(st.threadContext) >= maxThreadContexts {
			return
		}
	}
	st.threadContext[tid] = next
}

func packageFromOpenedPath(pidRoot string, eventType model.EventType, path string) (pkg, version string, ok bool) {
	if eventType != model.EventFileOpen {
		return "", "", false
	}
	if strings.Contains(path, "/node_modules/") {
		if p, v, ok := PathToPackage(pidRoot, path); ok {
			return p, v, true
		}
		if p, _, _ := splitNodeModules(path); p != "" {
			return p, "", true
		}
		return "", "", false
	}
	if strings.Contains(path, "/site-packages/") || strings.Contains(path, "/dist-packages/") {
		if p, v, ok := PathToPyPackage(pidRoot, path); ok {
			return p, v, true
		}
		if ir, _, _, ok := splitSitePackages(path); ok && ir != "" {
			return ir, "", true
		}
	}
	if strings.Contains(path, "/skills/") {
		if p, v, ok := PathToOpenClawSkill(pidRoot, path); ok {
			return p, v, true
		}
	}
	return "", "", false
}

func contextEligible(ev *model.RawEvent) bool {
	switch model.EventType(ev.Type) {
	case model.EventNetConnect, model.EventProcExec, model.EventDenyConnect, model.EventDenyExec:
		return true
	case model.EventFileOpen, model.EventDenyFileOpen:
		return IsSensitivePath(ev.ArgString())
	default:
		return false
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
	if service := processEnvValue(procRoot, pid, "GOODMAN_SERVICE"); service != "" {
		return service
	}
	cg, _ := os.ReadFile(fmt.Sprintf("%s/%d/cgroup", procRoot, pid))
	if strings.Contains(string(cg), "kubepods") {
		if hostname := processEnvValue(procRoot, pid, "HOSTNAME"); hostname != "" {
			return hostname
		}
	}
	if cwd, err := os.Readlink(fmt.Sprintf("%s/%d/cwd", procRoot, pid)); err == nil {
		if b := filepath.Base(cwd); b != "/" && b != "." {
			return b
		}
	}
	return fmt.Sprintf("pid-%d", pid)
}

func processEnvValue(procRoot string, pid int, key string) string {
	env, err := os.ReadFile(fmt.Sprintf("%s/%d/environ", procRoot, pid))
	if err != nil {
		return ""
	}
	prefix := key + "="
	for _, entry := range strings.Split(string(env), "\x00") {
		if value, ok := strings.CutPrefix(entry, prefix); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
