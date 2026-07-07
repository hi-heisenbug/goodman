package attribute

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PathToPackage turns "/app/node_modules/@scope/name/dist/x.js" into
// ("@scope/name", "1.2.3"). Handles nested and scoped packages: the LAST
// "/node_modules/" segment is the deepest — the actual actor.
// pidRoot handles container filesystems: pass "/proc/<pid>/root" so the
// package.json is read from the target's own mount namespace.
func PathToPackage(pidRoot, path string) (pkg, version string, ok bool) {
	idx := strings.LastIndex(path, "/node_modules/")
	if idx == -1 {
		return "", "", false
	}
	rest := path[idx+len("/node_modules/"):]
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}
	if strings.HasPrefix(parts[0], "@") {
		if len(parts) < 2 || parts[1] == "" {
			return "", "", false
		}
		pkg = parts[0] + "/" + parts[1] // scoped: @scope/name
	} else {
		pkg = parts[0]
	}
	pkgJSON := filepath.Join(pidRoot, path[:idx], "node_modules", pkg, "package.json")
	version = readVersionField(pkgJSON)
	return pkg, version, version != ""
}

var (
	versionCacheMu sync.Mutex
	versionCache   = map[string]string{} // package.json path -> version
)

func readVersionField(path string) string {
	versionCacheMu.Lock()
	if v, hit := versionCache[path]; hit {
		versionCacheMu.Unlock()
		return v
	}
	versionCacheMu.Unlock()

	v := ""
	if data, err := os.ReadFile(path); err == nil {
		var pj struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(data, &pj) == nil {
			v = pj.Version
		}
	}
	// Cache misses too: package.json does not change while a pid lives,
	// and retrying a missing file per-event would be a hot-path stat storm.
	versionCacheMu.Lock()
	versionCache[path] = v
	versionCacheMu.Unlock()
	return v
}

// FlushVersionCache drops the package.json cache (used when a watched pid
// exits — its /proc/<pid>/root paths become invalid).
func FlushVersionCache() {
	versionCacheMu.Lock()
	versionCache = map[string]string{}
	versionCacheMu.Unlock()
}
