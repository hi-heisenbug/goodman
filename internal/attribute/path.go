package attribute

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hi-heisenbug/goodman/internal/model"
)

const maxPathSymlinks = 40

// resolveProcessPath converts a syscall path into the absolute, symlink-resolved
// path visible inside the target process's mount namespace. /proc/<pid>/root
// keeps filesystem lookups in that namespace without changing the sensor's own
// mount namespace. Failure returns the original argument and false so callers
// can retain detection while refusing to compile an enforcement verdict.
func resolveProcessPath(procRoot string, pid int, dirFD int32, raw string) (string, bool) {
	if raw == "" {
		return raw, false
	}
	candidate := raw
	if !filepath.IsAbs(candidate) {
		base, ok := processPathBase(procRoot, pid, dirFD)
		if !ok {
			return raw, false
		}
		candidate = filepath.Join(base, candidate)
	}
	candidate = filepath.Clean(candidate)
	if !filepath.IsAbs(candidate) {
		return raw, false
	}
	resolved, err := resolveContainerSymlinks(filepath.Join(procRoot, strconv.Itoa(pid), "root"), candidate)
	if err != nil {
		return candidate, false
	}
	return resolved, true
}

func processPathBase(procRoot string, pid int, dirFD int32) (string, bool) {
	var link string
	switch {
	case dirFD == model.ATFDCWD:
		link = filepath.Join(procRoot, strconv.Itoa(pid), "cwd")
	case dirFD >= 0:
		link = filepath.Join(procRoot, strconv.Itoa(pid), "fd", strconv.FormatInt(int64(dirFD), 10))
	default:
		return "", false
	}
	base, err := os.Readlink(link)
	if err != nil || !filepath.IsAbs(base) || strings.HasSuffix(base, " (deleted)") {
		return "", false
	}
	return filepath.Clean(base), true
}

func resolveContainerSymlinks(root, virtualPath string) (string, error) {
	pending := virtualPathParts(virtualPath)
	resolved := make([]string, 0, len(pending))
	symlinks := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(resolved) > 0 {
				resolved = resolved[:len(resolved)-1]
			}
			continue
		}
		hostPath := filepath.Join(append([]string{root}, append(resolved, part)...)...)
		info, err := os.Lstat(hostPath)
		if err != nil {
			if os.IsNotExist(err) && len(pending) == 0 {
				resolved = append(resolved, part)
				break
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			resolved = append(resolved, part)
			continue
		}
		symlinks++
		if symlinks > maxPathSymlinks {
			return "", fmt.Errorf("too many symlinks")
		}
		target, err := os.Readlink(hostPath)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(target) {
			resolved = resolved[:0]
		}
		pending = append(virtualPathParts(target), pending...)
	}
	if len(resolved) == 0 {
		return "/", nil
	}
	return "/" + filepath.ToSlash(filepath.Join(resolved...)), nil
}

func virtualPathParts(path string) []string {
	return strings.Split(filepath.ToSlash(path), "/")
}

func unresolvedPath(path string) string {
	return "<unresolved>:" + path
}
