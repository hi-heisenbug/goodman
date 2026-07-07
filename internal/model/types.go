// Package model holds the shared data types that flow through Goodman:
// raw kernel events, attributed events, fingerprints and alerts.
package model

import (
	"bytes"
	"fmt"
	"time"
)

type EventType uint8

const (
	EventFileOpen   EventType = 1
	EventNetConnect EventType = 2
	EventProcExec   EventType = 3
)

func (t EventType) String() string {
	switch t {
	case EventFileOpen:
		return "FILE_OPEN"
	case EventNetConnect:
		return "NET_CONNECT"
	case EventProcExec:
		return "PROC_EXEC"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", uint8(t))
	}
}

const (
	TaskCommLen   = 16
	MaxStackDepth = 32
	PathMaxLen    = 256
)

// RawEvent mirrors struct event in bpf/goodman.h byte-for-byte
// (little-endian, natural alignment, explicit padding).
type RawEvent struct {
	PID       uint32
	TID       uint32
	Type      uint8
	Comm      [TaskCommLen]byte
	Arg       [PathMaxLen]byte
	Pad       [3]byte
	StackLen  uint32
	Stack     [MaxStackDepth]uint64
	Timestamp uint64
}

// RawEventSize is the wire size of struct event. Kept as a constant so the
// ring-buffer reader can sanity-check record lengths against the C side.
const RawEventSize = 4 + 4 + 1 + TaskCommLen + PathMaxLen + 3 + 4 + 8*MaxStackDepth + 8

func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

func (e *RawEvent) CommString() string { return cstr(e.Comm[:]) }
func (e *RawEvent) ArgString() string  { return cstr(e.Arg[:]) }

// UserStack returns the valid portion of the captured user stack.
func (e *RawEvent) UserStack() []uint64 {
	n := e.StackLen
	if n > MaxStackDepth {
		n = MaxStackDepth
	}
	return e.Stack[:n]
}

// Attributed is a RawEvent after we've figured out which package caused it.
type Attributed struct {
	Service   string    `json:"service"`   // k8s service / pod name (or cwd basename locally)
	Package   string    `json:"package"`   // e.g. "@tanstack/react-router"; "<app>" if app code
	Version   string    `json:"version"`   // e.g. "1.120.17"; "" if unknown
	Type      EventType `json:"type"`
	Behavior  string    `json:"behavior"`  // canonicalized: "READ /app/src/**" or "CONNECT 1.2.3.4:443"
	Timestamp uint64    `json:"timestamp"` // ns since boot converted to unix ns by the sensor
}

// EventBatch is what the sensor POSTs to the collector.
type EventBatch struct {
	Sensor string       `json:"sensor"` // node/host name
	Events []Attributed `json:"events"`
}

// BehaviorStat records how often and when one behavior was seen.
type BehaviorStat struct {
	Count     int    `json:"count"`
	FirstSeen uint64 `json:"first"` // unix ns
	LastSeen  uint64 `json:"last"`  // unix ns
}

// Fingerprint is the set of behaviors seen for one (service, package, version).
type Fingerprint struct {
	Service    string                  `json:"service"`
	Package    string                  `json:"package"`
	Version    string                  `json:"version"`
	Behaviors  map[string]BehaviorStat `json:"behaviors"`
	FirstSeen  uint64                  `json:"first_seen"`
	LastSeen   uint64                  `json:"last_seen"`
	ObsCount   int                     `json:"obs_count"`
	IsBaseline bool                    `json:"is_baseline"`
}

// Alert is emitted by the diff engine.
type Alert struct {
	ID           string   `json:"id"`
	Service      string   `json:"service"`
	Package      string   `json:"package"`
	OldVersion   string   `json:"old_version"`
	NewVersion   string   `json:"new_version"`
	Severity     string   `json:"severity"` // INFO | WARN | CRITICAL
	NewBehaviors []string `json:"new_behaviors"`
	DetectedAt   uint64   `json:"detected_at"` // unix ns
	Status       string   `json:"status"`      // open | acknowledged | resolved
}

const (
	SeverityInfo     = "INFO"
	SeverityWarn     = "WARN"
	SeverityCritical = "CRITICAL"
)

const (
	AlertOpen         = "open"
	AlertAcknowledged = "acknowledged"
	AlertResolved     = "resolved"
)

// NsToTime converts unix nanoseconds to time.Time.
func NsToTime(ns uint64) time.Time { return time.Unix(0, int64(ns)) }
