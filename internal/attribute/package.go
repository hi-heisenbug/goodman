package attribute

import (
	"bufio"
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

// PathToPyPackage turns ".../site-packages/requests/adapters.py" into
// ("requests", "2.34.2"). Uses the LAST site-packages/dist-packages segment
// and resolves version via adjacent *.dist-info (METADATA / top_level.txt).
// Ambiguity or missing metadata → ok=false (caller may still use the import
// root as package-without-version). Never guesses a version.
func PathToPyPackage(pidRoot, path string) (pkg, version string, ok bool) {
	importRoot, siteParent, marker, okSplit := splitSitePackages(path)
	if !okSplit || importRoot == "" {
		return "", "", false
	}
	siteDir := filepath.Join(pidRoot, siteParent, marker)
	distName, ver, found := lookupDistInfo(siteDir, importRoot)
	if !found || ver == "" {
		return importRoot, "", false
	}
	if distName == "" {
		distName = importRoot
	}
	return distName, ver, true
}

// splitSitePackages finds the deepest site-packages or dist-packages segment.
// importRoot is the next path component (or the .py basename for single-file
// modules like site-packages/six.py).
func splitSitePackages(path string) (importRoot, siteParent, marker string, ok bool) {
	idxSite := strings.LastIndex(path, "/site-packages/")
	idxDist := strings.LastIndex(path, "/dist-packages/")
	idx := idxSite
	marker = "site-packages"
	if idxDist > idx {
		idx = idxDist
		marker = "dist-packages"
	}
	if idx < 0 {
		return "", "", "", false
	}
	siteParent = path[:idx]
	tail := path[idx+len("/"+marker+"/"):]
	if tail == "" {
		return "", "", "", false
	}
	parts := strings.SplitN(tail, "/", 2)
	root := parts[0]
	if root == "" {
		return "", "", "", false
	}
	if strings.HasSuffix(root, ".py") && (len(parts) == 1 || parts[1] == "") {
		root = strings.TrimSuffix(root, ".py")
	}
	return root, siteParent, marker, true
}

type distInfoEntry struct {
	name     string
	version  string
	topLevel []string
}

var (
	versionCacheMu sync.Mutex
	versionCache   = map[string]string{} // package.json path -> version

	distInfoMu    sync.Mutex
	distInfoCache = map[string][]distInfoEntry{} // siteDir -> entries
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

func lookupDistInfo(siteDir, importRoot string) (distName, version string, ok bool) {
	entries := loadDistInfoIndex(siteDir)
	normImport := pep503Normalize(importRoot)

	var matches []distInfoEntry
	for _, e := range entries {
		for _, top := range e.topLevel {
			if pep503Normalize(top) == normImport {
				matches = append(matches, e)
				break
			}
		}
	}
	if len(matches) == 0 {
		for _, e := range entries {
			if pep503Normalize(e.name) == normImport {
				matches = append(matches, e)
			}
		}
	}
	if len(matches) != 1 {
		return "", "", false
	}
	return matches[0].name, matches[0].version, true
}

func loadDistInfoIndex(siteDir string) []distInfoEntry {
	distInfoMu.Lock()
	if cached, hit := distInfoCache[siteDir]; hit {
		distInfoMu.Unlock()
		return cached
	}
	distInfoMu.Unlock()

	var out []distInfoEntry
	ents, err := os.ReadDir(siteDir)
	if err == nil {
		for _, ent := range ents {
			name := ent.Name()
			if !ent.IsDir() || !strings.HasSuffix(name, ".dist-info") {
				continue
			}
			base := strings.TrimSuffix(name, ".dist-info")
			distName, ver := splitDistInfoDir(base)
			dir := filepath.Join(siteDir, name)
			if n, v := readDistMetadata(filepath.Join(dir, "METADATA")); n != "" || v != "" {
				if n != "" {
					distName = n
				}
				if v != "" {
					ver = v
				}
			}
			tops := readTopLevel(filepath.Join(dir, "top_level.txt"))
			if len(tops) == 0 && distName != "" {
				tops = []string{strings.ReplaceAll(pep503Normalize(distName), "-", "_")}
				// Also try the normalized dist name as an import hint.
				tops = append(tops, pep503Normalize(distName))
			}
			out = append(out, distInfoEntry{name: distName, version: ver, topLevel: tops})
		}
	}

	distInfoMu.Lock()
	distInfoCache[siteDir] = out
	distInfoMu.Unlock()
	return out
}

func splitDistInfoDir(base string) (name, version string) {
	// PEP 427: "{distribution}-{version}.dist-info"
	i := strings.LastIndex(base, "-")
	if i <= 0 || i == len(base)-1 {
		return base, ""
	}
	// version often starts with a digit; if not, still split on last '-'
	return base[:i], base[i+1:]
}

func readDistMetadata(path string) (name, version string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break // end of headers
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "name:") {
			if idx := strings.Index(line, ":"); idx >= 0 {
				name = strings.TrimSpace(line[idx+1:])
			}
		}
		if strings.HasPrefix(lower, "version:") {
			if idx := strings.Index(line, ":"); idx >= 0 {
				version = strings.TrimSpace(line[idx+1:])
			}
		}
	}
	return name, version
}

func readTopLevel(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// pep503Normalize lowercases and maps [-_.] to '-' (PEP 503).
func pep503Normalize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '-', '_', '.':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FlushVersionCache drops package.json and dist-info caches (used when a
// watched pid exits — its /proc/<pid>/root paths become invalid).
func FlushVersionCache() {
	versionCacheMu.Lock()
	versionCache = map[string]string{}
	versionCacheMu.Unlock()
	distInfoMu.Lock()
	distInfoCache = map[string][]distInfoEntry{}
	distInfoMu.Unlock()
}
