// Package enforce compiles literal kernel deny verdicts from rules and
// observed behavior strings. The kernel does exact lookups only.
package enforce

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/hi-heisenbug/goodman/internal/diff"
)

// ConnectVerdict is one connect deny entry (port 0 = any port).
type ConnectVerdict struct {
	Addr string `json:"addr"`
	Port uint16 `json:"port"`
}

// SkippedVerdict records a behavior that could not become a literal verdict.
type SkippedVerdict struct {
	Behavior string `json:"behavior"`
	Reason   string `json:"reason"`
}

// VerdictSet is the compiled deny map contents distributed to sensors.
type VerdictSet struct {
	Open    []string         `json:"open"`
	Connect []ConnectVerdict `json:"connect"`
	Exec    []string         `json:"exec"`
	Skipped []SkippedVerdict `json:"skipped,omitempty"`
}

// ServiceVerdicts keeps compiled literals isolated by Goodman's service name.
// Sensors expand each service's literals only into cgroup keys belonging to
// that service on the local node.
type ServiceVerdicts map[string]VerdictSet

// CompileVerdicts returns literal deny entries for behaviors matching block
// rules. Excludes on rules suppress compilation at compile time.
func CompileVerdicts(rules []diff.Rule, behaviors []string) VerdictSet {
	var vs VerdictSet
	seenOpen := map[string]bool{}
	seenConnect := map[string]bool{}
	seenExec := map[string]bool{}
	seenSkip := map[string]bool{}

	for _, b := range behaviors {
		var matchedBlock bool
		for i := range rules {
			r := &rules[i]
			if r.Action != diff.ActionBlock {
				continue
			}
			if !r.Matches(b) {
				continue
			}
			matchedBlock = true
			break
		}
		if !matchedBlock {
			continue
		}
		switch {
		case strings.HasPrefix(b, "READ "):
			path := strings.TrimPrefix(b, "READ ")
			if !compileOpen(path) {
				addSkip(&vs, seenSkip, b, "not an absolute literal path")
				continue
			}
			if !seenOpen[path] {
				seenOpen[path] = true
				vs.Open = append(vs.Open, path)
			}
		case strings.HasPrefix(b, "CONNECT "):
			addr, port, ok := parseConnectLiteral(strings.TrimPrefix(b, "CONNECT "))
			if !ok {
				addSkip(&vs, seenSkip, b, "not a literal address")
				continue
			}
			key := fmt.Sprintf("%s:%d", addr, port)
			if !seenConnect[key] {
				seenConnect[key] = true
				vs.Connect = append(vs.Connect, ConnectVerdict{Addr: addr, Port: port})
			}
		case strings.HasPrefix(b, "EXEC "):
			path := strings.TrimPrefix(b, "EXEC ")
			if !compileExec(path) {
				addSkip(&vs, seenSkip, b, "not an absolute literal path")
				continue
			}
			if !seenExec[path] {
				seenExec[path] = true
				vs.Exec = append(vs.Exec, path)
			}
		default:
			addSkip(&vs, seenSkip, b, "unsupported behavior class")
		}
	}
	return vs
}

func addSkip(vs *VerdictSet, seen map[string]bool, behavior, reason string) {
	if seen[behavior] {
		return
	}
	seen[behavior] = true
	vs.Skipped = append(vs.Skipped, SkippedVerdict{Behavior: behavior, Reason: reason})
}

func compileOpen(path string) bool {
	if path == "" || len(path) > 255 {
		return false
	}
	if !filepath.IsAbs(path) {
		return false
	}
	if strings.Contains(path, "**") || strings.Contains(path, "<") {
		return false
	}
	return true
}

func compileExec(path string) bool {
	return compileOpen(path)
}

func parseConnectLiteral(arg string) (addr string, port uint16, ok bool) {
	if arg == "" || strings.Contains(arg, "/") {
		return "", 0, false
	}
	// IPv6: [addr]:port
	if strings.HasPrefix(arg, "[") {
		end := strings.Index(arg, "]:")
		if end < 0 {
			return "", 0, false
		}
		host := arg[1:end]
		portStr := arg[end+2:]
		if net.ParseIP(host) == nil {
			return "", 0, false
		}
		p, err := parsePort(portStr)
		if err != nil {
			return "", 0, false
		}
		return host, p, true
	}
	// IPv4: ip:port
	i := strings.LastIndex(arg, ":")
	if i < 0 {
		return "", 0, false
	}
	host, portStr := arg[:i], arg[i+1:]
	if net.ParseIP(host) == nil {
		return "", 0, false
	}
	p, err := parsePort(portStr)
	if err != nil {
		return "", 0, false
	}
	return host, p, true
}

func parsePort(s string) (uint16, error) {
	var p int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bad port")
		}
		p = p*10 + int(c-'0')
		if p > 65535 {
			return 0, fmt.Errorf("bad port")
		}
	}
	return uint16(p), nil
}
