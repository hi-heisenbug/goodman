package attribute

import (
	"path/filepath"
	"strings"

	"github.com/hi-heisenbug/goodman/internal/model"
)

// Canonicalize maps a raw syscall argument to a stable behavior string so
// the same logical behavior always produces the same fingerprint entry.
//
//	FILE_OPEN  -> "READ /app/src/routes/**"      (dir-collapsed)
//	           -> "READ /var/run/secrets/k8s.io/token"  (sensitive: verbatim)
//	NET_CONNECT-> "CONNECT 1.2.3.4:443"
//	PROC_EXEC  -> "EXEC curl"
func Canonicalize(t model.EventType, arg string) string {
	switch t {
	case model.EventFileOpen:
		return "READ " + canonicalPath(arg)
	case model.EventNetConnect:
		return "CONNECT " + arg
	case model.EventProcExec:
		if arg == "" {
			return "EXEC <unknown>"
		}
		return "EXEC " + filepath.Base(arg)
	default:
		return "UNKNOWN " + arg
	}
}

// sensitiveMarkers: any path containing one of these is kept verbatim —
// collapsing would hide exactly the reads we most need to alert on.
var sensitiveMarkers = []string{
	"secret", "token", "credential", "password", "passwd", "shadow",
	".pem", ".key", ".aws", ".ssh", ".npmrc", ".env", "id_rsa", "kubeconfig",
}

// IsSensitivePath reports whether a filesystem path must never be collapsed.
func IsSensitivePath(p string) bool {
	lp := strings.ToLower(p)
	for _, m := range sensitiveMarkers {
		if strings.Contains(lp, m) {
			return true
		}
	}
	return strings.HasPrefix(p, "/var/run/secrets/") || strings.HasPrefix(p, "/run/secrets/")
}

// canonicalPath collapses the variable last segment of a path:
// /app/src/routes/user-42.js -> /app/src/routes/**
// Sensitive paths stay verbatim. node_modules paths collapse to the package
// dir so per-file reads inside a dependency don't explode the behavior set.
func canonicalPath(p string) string {
	if p == "" {
		return "<unknown>"
	}
	p = filepath.Clean(p)
	if IsSensitivePath(p) {
		return p
	}
	if pkg, dir, _ := splitNodeModules(p); pkg != "" {
		return dir + "/node_modules/" + pkg + "/**"
	}
	// Shallow paths (/etc/hosts, /etc/passwd) stay verbatim: low cardinality,
	// high signal. Only collapse the variable tail of deeper paths.
	if strings.Count(p, "/") <= 2 {
		return p
	}
	dir := filepath.Dir(p)
	if dir == "/" || dir == "." {
		return p
	}
	return dir + "/**"
}

// CloudMetadataIP is the link-local cloud metadata endpoint — always kept
// verbatim and treated as high risk by the default rules.
const CloudMetadataIP = "169.254.169.254"
