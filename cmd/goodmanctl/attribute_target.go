package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hi-heisenbug/goodman/internal/attribute"
	"github.com/hi-heisenbug/goodman/internal/loader"
)

type attributeTarget struct {
	PID     int
	Comm    string
	Command string
}

func resolveAttributeTarget(procRoot string, requestedPID int) (attributeTarget, error) {
	if requestedPID > 0 {
		return readAttributeTarget(procRoot, requestedPID)
	}
	targets, err := discoverAttributeTargets(procRoot)
	if err != nil {
		return attributeTarget{}, err
	}
	if len(targets) == 1 {
		return targets[0], nil
	}
	if len(targets) == 0 {
		return attributeTarget{}, fmt.Errorf("no supported Node/Python process found; start the workload, then retry or pass -pid")
	}
	return attributeTarget{}, multipleTargetsError(targets)
}

func discoverAttributeTargets(procRoot string) ([]attributeTarget, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", procRoot, err)
	}
	targets := make([]attributeTarget, 0)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		target, err := readAttributeTarget(procRoot, pid)
		if err == nil && loader.WatchedComms[target.Comm] {
			targets = append(targets, target)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].PID < targets[j].PID })
	return targets, nil
}

func readAttributeTarget(procRoot string, pid int) (attributeTarget, error) {
	pidDir := filepath.Join(procRoot, strconv.Itoa(pid))
	comm, err := os.ReadFile(filepath.Join(pidDir, "comm"))
	if err != nil {
		return attributeTarget{}, fmt.Errorf("read pid %d: %w", pid, err)
	}
	command, _ := os.ReadFile(filepath.Join(pidDir, "cmdline"))
	display := displayCommand(command)
	if display == "" {
		display = strings.TrimSpace(string(comm))
	}
	return attributeTarget{
		PID:     pid,
		Comm:    strings.TrimSpace(string(comm)),
		Command: display,
	}, nil
}

func displayCommand(raw []byte) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(string(raw), "\x00", " ")), " ")
}

func multipleTargetsError(targets []attributeTarget) error {
	var message strings.Builder
	message.WriteString("multiple supported runtimes found; rerun with -pid from this list:\n")
	for _, target := range targets {
		fmt.Fprintf(&message, "  %-7d %-16s %s\n", target.PID, target.Comm, target.Command)
	}
	return errors.New(strings.TrimSpace(message.String()))
}

func attributeTargetReadiness(procRoot string, target attributeTarget) (bool, string) {
	nsPID := attribute.NSPID(procRoot, target.PID)
	perfMap := attribute.PerfMapPath(procRoot, target.PID, nsPID)
	if info, err := os.Stat(perfMap); err == nil && !info.IsDir() {
		return true, fmt.Sprintf("Tier-1 attribution ready: %s", perfMap)
	}
	settings := target.Command + " " + readTargetEnvironment(procRoot, target.PID)
	if isNodeTarget(target.Comm) {
		return nodeTargetReadiness(settings)
	}
	if isPythonTarget(target.Comm) {
		return pythonTargetReadiness(settings)
	}
	return false, "Tier-1 attribution map is not present; exact dependency attribution may remain <unknown>"
}

func readTargetEnvironment(procRoot string, pid int) string {
	raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "environ"))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(raw), "\x00", " ")
}

func nodeTargetReadiness(settings string) (bool, string) {
	const basicProf = "--perf-basic-prof-only-functions"
	const nativeStack = "--interpreted-frames-native-stack"
	if strings.Contains(settings, basicProf) && strings.Contains(settings, nativeStack) {
		return false, "Node profiling flags are configured; generate traffic so V8 emits its perf map during the trace"
	}
	return false, "restart Node with NODE_OPTIONS=\"--perf-basic-prof-only-functions --interpreted-frames-native-stack\" for exact package attribution"
}

func pythonTargetReadiness(settings string) (bool, string) {
	if strings.Contains(settings, "PYTHONPERFSUPPORT=1") {
		return false, "PYTHONPERFSUPPORT=1 is configured; generate traffic so Python emits its perf map during the trace"
	}
	return false, "restart Python with PYTHONPERFSUPPORT=1 for exact package attribution"
}

func isNodeTarget(comm string) bool {
	switch comm {
	case "node", "nodejs", "MainThread", "openclaw-gatewa":
		return true
	default:
		return false
	}
}

func isPythonTarget(comm string) bool {
	if strings.HasPrefix(comm, "python") {
		return true
	}
	switch comm {
	case "gunicorn", "celery", "uwsgi", "uvicorn":
		return true
	default:
		return false
	}
}
