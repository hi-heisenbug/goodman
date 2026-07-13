// Package report builds the runtime reachability report: which declared npm
// dependencies actually executed in production, cross-referenced with known
// vulnerabilities. It answers the daily question ("which of my thousands of
// dependencies matter?") that sits next to Goodman's drift detection.
package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DeclaredPackage is one dependency named in a lockfile.
type DeclaredPackage struct {
	Name    string
	Version string
	Dev     bool // declared as a dev-only dependency
}

// ParseLockfile parses an npm package-lock.json (v1, v2, or v3) into the set
// of declared packages. v2/v3 use the "packages" map keyed by install path;
// v1 uses the nested "dependencies" tree. Both are handled.
func ParseLockfile(data []byte) ([]DeclaredPackage, error) {
	var lf struct {
		LockfileVersion int `json:"lockfileVersion"`
		Packages        map[string]struct {
			Version string `json:"version"`
			Dev     bool   `json:"dev"`
		} `json:"packages"`
		Dependencies map[string]json.RawMessage `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parse lockfile: %w", err)
	}

	seen := map[string]DeclaredPackage{}
	// v2/v3: the authoritative "packages" map. Keys look like
	// "node_modules/foo" or "node_modules/foo/node_modules/bar"; the root
	// project is the "" key and is skipped.
	for path, meta := range lf.Packages {
		name := packageNameFromPath(path)
		if name == "" {
			continue
		}
		key := name + "@" + meta.Version
		if existing, ok := seen[key]; !ok || (existing.Dev && !meta.Dev) {
			seen[key] = DeclaredPackage{Name: name, Version: meta.Version, Dev: meta.Dev}
		}
	}
	// v1 fallback: walk the nested dependencies tree.
	if len(lf.Packages) == 0 {
		if err := walkV1(lf.Dependencies, seen); err != nil {
			return nil, err
		}
	}

	out := make([]DeclaredPackage, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out, nil
}

// packageNameFromPath extracts the package name from a v2/v3 install path,
// honoring the deepest node_modules segment and scoped (@scope/name) packages.
func packageNameFromPath(path string) string {
	const marker = "node_modules/"
	i := strings.LastIndex(path, marker)
	if i < 0 {
		return "" // "" root key, or a workspace path
	}
	name := path[i+len(marker):]
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "@") {
		// scope/name: keep the first two segments.
		parts := strings.SplitN(name, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return name
	}
	// Unscoped: keep the first segment (defensive against odd keys).
	return strings.SplitN(name, "/", 2)[0]
}

func walkV1(deps map[string]json.RawMessage, seen map[string]DeclaredPackage) error {
	for name, raw := range deps {
		var node struct {
			Version      string                     `json:"version"`
			Dev          bool                       `json:"dev"`
			Dependencies map[string]json.RawMessage `json:"dependencies"`
		}
		if err := json.Unmarshal(raw, &node); err != nil {
			return fmt.Errorf("parse dependency %q: %w", name, err)
		}
		key := name + "@" + node.Version
		if existing, ok := seen[key]; !ok || (existing.Dev && !node.Dev) {
			seen[key] = DeclaredPackage{Name: name, Version: node.Version, Dev: node.Dev}
		}
		if len(node.Dependencies) > 0 {
			if err := walkV1(node.Dependencies, seen); err != nil {
				return err
			}
		}
	}
	return nil
}
