package attribute

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPathToPackage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app/node_modules/express/package.json"), `{"name":"express","version":"4.19.2"}`)
	writeFile(t, filepath.Join(root, "app/node_modules/@scope/pkg/package.json"), `{"version":"2.0.1"}`)
	writeFile(t, filepath.Join(root, "app/node_modules/a/node_modules/b/package.json"), `{"version":"9.9.9"}`)

	cases := []struct {
		path        string
		wantPkg     string
		wantVersion string
		wantOK      bool
	}{
		{"/app/node_modules/express/lib/router/index.js", "express", "4.19.2", true},
		{"/app/node_modules/@scope/pkg/dist/x.mjs", "@scope/pkg", "2.0.1", true},
		{"/app/node_modules/a/node_modules/b/index.js", "b", "9.9.9", true}, // deepest wins
		{"/app/src/server.js", "", "", false},
		{"/app/node_modules/missing/index.js", "missing", "", false}, // no package.json -> not ok
	}
	for _, c := range cases {
		pkg, ver, ok := PathToPackage(root, c.path)
		if ok != c.wantOK || (ok && (pkg != c.wantPkg || ver != c.wantVersion)) || (!ok && pkg != "" && c.wantPkg == "") {
			t.Errorf("PathToPackage(%q) = (%q,%q,%v), want (%q,%q,%v)", c.path, pkg, ver, ok, c.wantPkg, c.wantVersion, c.wantOK)
		}
	}
}

func TestCanonicalize(t *testing.T) {
	cases := []struct {
		t    model.EventType
		arg  string
		want string
	}{
		{model.EventFileOpen, "/app/src/routes/user-42.js", "READ /app/src/routes/**"},
		{model.EventFileOpen, "/var/run/secrets/kubernetes.io/serviceaccount/token", "READ /var/run/secrets/kubernetes.io/serviceaccount/token"},
		{model.EventFileOpen, "/home/u/.aws/credentials", "READ /home/u/.aws/credentials"},
		{model.EventFileOpen, "/app/certs/server.pem", "READ /app/certs/server.pem"},
		{model.EventFileOpen, "/app/node_modules/express/lib/view.js", "READ /app/node_modules/express/**"},
		{model.EventFileOpen, "/app/venv/lib/python3.13/site-packages/requests/adapters.py", "READ /app/venv/lib/python3.13/site-packages/requests/**"},
		{model.EventFileOpen, "/usr/lib/python3/dist-packages/yaml/loader.py", "READ /usr/lib/python3/dist-packages/yaml/**"},
		{model.EventFileOpen, "/etc/hosts", "READ /etc/hosts"},
		{model.EventNetConnect, "140.82.113.6:443", "CONNECT 140.82.113.6:443"},
		{model.EventProcExec, "/usr/bin/curl", "EXEC /usr/bin/curl"},
	}
	for _, c := range cases {
		if got := Canonicalize(c.t, c.arg); got != c.want {
			t.Errorf("Canonicalize(%v,%q) = %q, want %q", c.t, c.arg, got, c.want)
		}
	}
}

func TestCanonicalizeConnectCIDR(t *testing.T) {
	cases := []struct {
		arg  string
		bits int
		want string
	}{
		// Public IPv4 collapses to the requested prefix.
		{"52.84.12.7:443", 16, "CONNECT 52.84.0.0/16:443"},
		{"52.84.250.9:443", 16, "CONNECT 52.84.0.0/16:443"}, // same /16 as above: dedups CDN rotation
		{"140.82.113.6:443", 24, "CONNECT 140.82.113.0/24:443"},
		// Aggregation off (0) keeps exact IPs.
		{"52.84.12.7:443", 0, "CONNECT 52.84.12.7:443"},
		// Private, loopback, link-local stay exact even with aggregation on.
		{"10.0.0.5:5432", 16, "CONNECT 10.0.0.5:5432"},
		{"192.168.1.9:80", 16, "CONNECT 192.168.1.9:80"},
		{"127.0.0.1:6379", 16, "CONNECT 127.0.0.1:6379"},
		// Cloud metadata is never aggregated (rules must still match it exactly).
		{"169.254.169.254:80", 16, "CONNECT 169.254.169.254:80"},
		// Out-of-range bits are a no-op.
		{"52.84.12.7:443", 40, "CONNECT 52.84.12.7:443"},
		// Non-IPv4 / malformed passes through.
		{"example.com:443", 16, "CONNECT example.com:443"},
	}
	for _, c := range cases {
		got := CanonicalizeWith(model.EventNetConnect, c.arg, c.bits)
		if got != c.want {
			t.Errorf("CanonicalizeWith(CONNECT %q, bits=%d) = %q, want %q", c.arg, c.bits, got, c.want)
		}
	}
}

func TestPerfMapLookup(t *testing.T) {
	dir := t.TempDir()
	pm := filepath.Join(dir, "perf-1.map")
	writeFile(t, pm, ""+
		"3ca9f8c04a20 1e0 LazyCompile:*handleRequest /app/node_modules/@tanstack/react-router/dist/esm/router.js:412:19\n"+
		"3ca9f8c05000 100 LazyCompile:~anon /app/src/server.js:10:3\n"+
		"badline\n"+
		"3ca9f8c06000 80 RegExp:^foo$\n")

	p := NewPerfMap(pm)
	sym, ok := p.Lookup(0x3ca9f8c04a20 + 0x10)
	if !ok {
		t.Fatal("expected hit inside first interval")
	}
	src, ok := sourcePathOf(sym)
	if !ok || src != "/app/node_modules/@tanstack/react-router/dist/esm/router.js" {
		t.Fatalf("sourcePathOf(%q) = %q,%v", sym, src, ok)
	}
	if _, ok := p.Lookup(0x3ca9f8c04a20 + 0x1e0); ok {
		t.Fatal("end of interval is exclusive")
	}
	if _, ok := p.Lookup(0x1000); ok {
		t.Fatal("miss expected below all intervals")
	}
	if sym, ok := p.Lookup(0x3ca9f8c06000 + 1); !ok {
		t.Fatal("RegExp interval should resolve")
	} else if _, ok := sourcePathOf(sym); ok {
		t.Fatal("RegExp symbol has no source path")
	}
}

// TestAttributeEndToEnd simulates a full pid environment under a fake /proc.
func TestAttributeEndToEnd(t *testing.T) {
	proc := t.TempDir()
	pid := 4242
	pidDir := fmt.Sprintf("%s/%d", proc, pid)
	// fake rootfs of the target
	writeFile(t, pidDir+"/root/app/node_modules/good-pkg/package.json", `{"version":"1.0.1"}`)
	writeFile(t, pidDir+"/root/app/package.json", `{"version":"0.1.0"}`)
	writeFile(t, pidDir+"/root/tmp/perf-4242.map", ""+
		"5000 100 LazyCompile:*exfil /app/node_modules/good-pkg/index.js:5:10\n"+
		"6000 100 LazyCompile:*main /app/src/server.js:1:1\n")
	writeFile(t, pidDir+"/status", "Name:\tnode\nNSpid:\t4242\n")
	writeFile(t, pidDir+"/maps", "00400000-00500000 r-xp 00000000 08:01 1 /usr/bin/node\n")
	writeFile(t, pidDir+"/cgroup", "0::/user.slice\n")
	os.Symlink("/work/myservice", pidDir+"/cwd")

	r := NewResolver(proc)

	ev := &model.RawEvent{PID: uint32(pid), Type: uint8(model.EventNetConnect), StackLen: 3}
	copy(ev.Arg[:], "127.0.0.1:9999")
	ev.Stack[0] = 0x420000 // native node frame
	ev.Stack[1] = 0x5010   // good-pkg JIT frame  <- deepest node_modules actor
	ev.Stack[2] = 0x6010   // app frame above it

	got := r.Attribute(ev, 0)
	if got.Package != "good-pkg" || got.Version != "1.0.1" {
		t.Fatalf("attributed %q@%q, want good-pkg@1.0.1", got.Package, got.Version)
	}
	if got.Behavior != "CONNECT 127.0.0.1:9999" {
		t.Fatalf("behavior = %q", got.Behavior)
	}
	if got.Service != "myservice" {
		t.Fatalf("service = %q, want myservice (cwd basename)", got.Service)
	}

	// App-only stack attributes to <app>, never a wrong package.
	ev2 := &model.RawEvent{PID: uint32(pid), Type: uint8(model.EventFileOpen), StackLen: 1}
	copy(ev2.Arg[:], "/app/data/x.txt")
	ev2.Stack[0] = 0x6010
	got2 := r.Attribute(ev2, 0)
	if got2.Package != "<app>" || got2.Version != "0.1.0" {
		t.Fatalf("attributed %q@%q, want <app>@0.1.0", got2.Package, got2.Version)
	}

	// Fully unresolvable stack -> <unknown>, honest over wrong.
	ev3 := &model.RawEvent{PID: uint32(pid), Type: uint8(model.EventFileOpen), StackLen: 1}
	copy(ev3.Arg[:], "/etc/hosts")
	ev3.Stack[0] = 0xdeadbeef
	if got3 := r.Attribute(ev3, 0); got3.Package != "<unknown>" {
		t.Fatalf("attributed %q, want <unknown>", got3.Package)
	}
}

func TestAttributeUsesOpenedPackagePathAndShortThreadContext(t *testing.T) {
	proc := t.TempDir()
	pid := 4243
	pidDir := fmt.Sprintf("%s/%d", proc, pid)
	writeFile(t, pidDir+"/root/app/node_modules/good-pkg/package.json", `{"version":"1.0.1"}`)
	writeFile(t, pidDir+"/root/tmp/perf-4243.map", "")
	writeFile(t, pidDir+"/status", "Name:\tMainThread\nNSpid:\t4243\n")
	writeFile(t, pidDir+"/maps", "00400000-00500000 r-xp 00000000 08:01 1 /usr/bin/node\n")
	writeFile(t, pidDir+"/cgroup", "0::/user.slice\n")
	os.Symlink("/work/workload", pidDir+"/cwd")

	r := NewResolver(proc)
	tid := uint32(9001)
	baseTs := uint64(10_000_000_000)

	packageRead := &model.RawEvent{PID: uint32(pid), TID: tid, Type: uint8(model.EventFileOpen), Timestamp: baseTs}
	copy(packageRead.Arg[:], "/app/node_modules/good-pkg/data.json")
	got := r.Attribute(packageRead, 0)
	if got.Package != "good-pkg" || got.Version != "1.0.1" {
		t.Fatalf("package path fallback = %q@%q, want good-pkg@1.0.1", got.Package, got.Version)
	}

	ordinaryRead := &model.RawEvent{PID: uint32(pid), TID: tid, Type: uint8(model.EventFileOpen), Timestamp: baseTs + uint64(5*time.Millisecond)}
	copy(ordinaryRead.Arg[:], "/etc/localtime")
	got = r.Attribute(ordinaryRead, 0)
	if got.Package != "<unknown>" {
		t.Fatalf("ordinary file inherited context: %q", got.Package)
	}

	secretRead := &model.RawEvent{PID: uint32(pid), TID: tid, Type: uint8(model.EventFileOpen), Timestamp: baseTs + uint64(10*time.Millisecond)}
	copy(secretRead.Arg[:], "/tmp/goodman-fake-secrets/credentials")
	got = r.Attribute(secretRead, 0)
	if got.Package != "good-pkg" || got.Version != "1.0.1" {
		t.Fatalf("same-thread context for secret = %q@%q, want good-pkg@1.0.1", got.Package, got.Version)
	}

	connect := &model.RawEvent{PID: uint32(pid), TID: tid, Type: uint8(model.EventNetConnect), Timestamp: baseTs + uint64(20*time.Millisecond)}
	copy(connect.Arg[:], "127.0.0.1:9999")
	got = r.Attribute(connect, 0)
	if got.Package != "good-pkg" || got.Version != "1.0.1" {
		t.Fatalf("same-thread context for connect = %q@%q, want good-pkg@1.0.1", got.Package, got.Version)
	}

	otherThread := &model.RawEvent{PID: uint32(pid), TID: tid + 1, Type: uint8(model.EventNetConnect), Timestamp: baseTs + uint64(30*time.Millisecond)}
	copy(otherThread.Arg[:], "127.0.0.1:9999")
	got = r.Attribute(otherThread, 0)
	if got.Package != "<unknown>" {
		t.Fatalf("different thread inherited context: %q", got.Package)
	}

	expired := &model.RawEvent{PID: uint32(pid), TID: tid, Type: uint8(model.EventNetConnect), Timestamp: baseTs + uint64(packageContextTTL) + 1}
	copy(expired.Arg[:], "127.0.0.1:9999")
	got = r.Attribute(expired, 0)
	if got.Package != "<unknown>" {
		t.Fatalf("expired context attributed %q, want <unknown>", got.Package)
	}
}

func TestSourcePathOfPython(t *testing.T) {
	src, ok := sourcePathOf("py::<module>:/app/venv/lib/python3.13/site-packages/requests/__init__.py")
	if !ok || src != "/app/venv/lib/python3.13/site-packages/requests/__init__.py" {
		t.Fatalf("got %q,%v", src, ok)
	}
	if _, ok := sourcePathOf("py::_find_and_load:<frozen importlib._bootstrap>"); ok {
		t.Fatal("frozen symbols must not resolve")
	}
	if _, ok := sourcePathOf("py::tick:work.py"); ok {
		t.Fatal("relative paths must not resolve")
	}
}

func TestPathToPyPackage(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "app/venv/lib/python3.13/site-packages")
	writeFile(t, filepath.Join(site, "requests/__init__.py"), "")
	writeFile(t, filepath.Join(site, "requests-2.34.2.dist-info/METADATA"), "Name: requests\nVersion: 2.34.2\n")
	writeFile(t, filepath.Join(site, "requests-2.34.2.dist-info/top_level.txt"), "requests\n")
	writeFile(t, filepath.Join(site, "yaml/loader.py"), "")
	writeFile(t, filepath.Join(site, "PyYAML-6.0.dist-info/METADATA"), "Name: PyYAML\nVersion: 6.0\n")
	writeFile(t, filepath.Join(site, "PyYAML-6.0.dist-info/top_level.txt"), "yaml\n")
	writeFile(t, filepath.Join(site, "six.py"), "")
	writeFile(t, filepath.Join(site, "six-1.16.0.dist-info/METADATA"), "Name: six\nVersion: 1.16.0\n")
	writeFile(t, filepath.Join(site, "six-1.16.0.dist-info/top_level.txt"), "six\n")

	pkg, ver, ok := PathToPyPackage(root, "/app/venv/lib/python3.13/site-packages/requests/adapters.py")
	if !ok || pkg != "requests" || ver != "2.34.2" {
		t.Fatalf("requests = (%q,%q,%v)", pkg, ver, ok)
	}
	pkg, ver, ok = PathToPyPackage(root, "/app/venv/lib/python3.13/site-packages/yaml/loader.py")
	if !ok || pkg != "PyYAML" || ver != "6.0" {
		t.Fatalf("yaml→PyYAML = (%q,%q,%v)", pkg, ver, ok)
	}
	pkg, ver, ok = PathToPyPackage(root, "/app/venv/lib/python3.13/site-packages/six.py")
	if !ok || pkg != "six" || ver != "1.16.0" {
		t.Fatalf("six.py = (%q,%q,%v)", pkg, ver, ok)
	}
	FlushVersionCache()
	_ = os.RemoveAll(filepath.Join(site, "requests-2.34.2.dist-info"))
	pkg, ver, ok = PathToPyPackage(root, "/app/venv/lib/python3.13/site-packages/requests/adapters.py")
	if ok || pkg != "" && ver != "" && ok {
		// ok must be false without dist-info
	}
	if ok {
		t.Fatalf("missing dist-info must not ok, got (%q,%q)", pkg, ver)
	}
}

func TestAttributePythonEndToEnd(t *testing.T) {
	proc := t.TempDir()
	pid := 5252
	pidDir := fmt.Sprintf("%s/%d", proc, pid)
	site := pidDir + "/root/app/venv/lib/python3.13/site-packages"
	writeFile(t, site+"/requests/__init__.py", "")
	writeFile(t, site+"/requests-2.34.2.dist-info/METADATA", "Name: requests\nVersion: 2.34.2\n")
	writeFile(t, site+"/requests-2.34.2.dist-info/top_level.txt", "requests\n")
	writeFile(t, pidDir+"/root/tmp/perf-5252.map", ""+
		"5000 100 py::<module>:/app/venv/lib/python3.13/site-packages/requests/__init__.py\n"+
		"6000 100 py::_find_and_load:<frozen importlib._bootstrap>\n"+
		"7000 100 py::<module>:/app/work.py\n")
	writeFile(t, pidDir+"/status", "Name:\tpython3\nNSpid:\t5252\n")
	writeFile(t, pidDir+"/maps", "00400000-00500000 r-xp 00000000 08:01 1 /usr/bin/python3\n")
	writeFile(t, pidDir+"/cgroup", "0::/user.slice\n")
	_ = os.Symlink("/work/pysvc", pidDir+"/cwd")

	r := NewResolver(proc)
	ev := &model.RawEvent{PID: uint32(pid), TID: 1, Type: uint8(model.EventFileOpen), Timestamp: 1, StackLen: 2}
	ev.Stack[0] = 0x5000 + 1
	ev.Stack[1] = 0x7000 + 1
	copy(ev.Arg[:], "/tmp/x")
	got := r.Attribute(ev, 0)
	if got.Package != "requests" || got.Version != "2.34.2" {
		t.Fatalf("got %q@%q, want requests@2.34.2", got.Package, got.Version)
	}

	// Frozen-only + app frame → <app>
	ev2 := &model.RawEvent{PID: uint32(pid), TID: 2, Type: uint8(model.EventFileOpen), Timestamp: 2, StackLen: 2}
	ev2.Stack[0] = 0x6000 + 1
	ev2.Stack[1] = 0x7000 + 1
	got = r.Attribute(ev2, 0)
	if got.Package != "<app>" {
		t.Fatalf("app-only stack = %q, want <app>", got.Package)
	}
}

func TestThreadContextIsBoundedAndPrunesExpiredEntries(t *testing.T) {
	st := &pidState{threadContext: map[uint32]packageContext{}}
	for i := 0; i < maxThreadContexts; i++ {
		st.threadContext[uint32(i)] = packageContext{pkg: "old", timestamp: 1}
	}
	now := uint64(packageContextTTL) + 2
	rememberPackageContext(st, 99_999, packageContext{pkg: "fresh", timestamp: now})
	if len(st.threadContext) != 1 {
		t.Fatalf("thread contexts = %d, want expired entries pruned", len(st.threadContext))
	}
	if st.threadContext[99_999].pkg != "fresh" {
		t.Fatalf("fresh context missing: %+v", st.threadContext)
	}
}
