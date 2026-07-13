// Package coverage assembles the Coverage and trust panel: sensor health,
// attribution KPI, namespace injection gaps, and alert-budget burn rate.
package coverage

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hi-heisenbug/goodman/internal/digest"
	"github.com/hi-heisenbug/goodman/internal/model"
)

const (
	sensorStaleAfter = 2 * time.Minute
	rateWindow       = time.Minute
)

// Snapshot is the GET /v1/coverage response.
type Snapshot struct {
	Sensors     []SensorHealth      `json:"sensors"`
	Attribution AttributionKPI      `json:"attribution"`
	Namespaces  []NamespaceCoverage `json:"namespaces"`
	AlertBudget AlertBudget         `json:"alert_budget"`
}

// SensorHealth is one node's last-seen state from ingest/heartbeats.
type SensorHealth struct {
	Name         string  `json:"name"`
	Status       string  `json:"status"` // running | stale | unknown
	LastSeen     uint64  `json:"last_seen"`
	EventsPerSec float64 `json:"events_per_sec"`
	EventsTotal  uint64  `json:"events_total"`
}

// AttributionKPI mirrors docs/performance.md: package / (package+app+unknown).
type AttributionKPI struct {
	Package     uint64           `json:"package"`
	App         uint64           `json:"app"`
	Unknown     uint64           `json:"unknown"`
	SuccessRate float64          `json:"success_rate"`
	TopUnknown  []UnknownService `json:"top_unknown"`
}

// UnknownService is one service contributing <unknown> attributions.
type UnknownService struct {
	Service string `json:"service"`
	Count   uint64 `json:"count"`
}

// NamespaceCoverage is injection coverage for one namespace.
type NamespaceCoverage struct {
	Name                string `json:"name"`
	InjectLabel         bool   `json:"inject_label"`
	PodsTotal           int    `json:"pods_total"`
	PodsWithNodeOptions int    `json:"pods_with_node_options"`
	PodsWithout         int    `json:"pods_without"`
	ReportedBy          string `json:"reported_by,omitempty"`
	ReportedAt          uint64 `json:"reported_at,omitempty"`
}

// AlertBudget is alerts in the last 24h against the soft daily target.
type AlertBudget struct {
	TargetPerDay  int `json:"target_per_day"`
	AlertsLast24h int `json:"alerts_last_24h"`
}

// Registry is an in-memory coverage state updated from ingest and coverage POSTs.
type Registry struct {
	mu sync.Mutex

	sensors    map[string]*sensorState
	attrPkg    uint64
	attrApp    uint64
	attrUnk    uint64
	unkBySvc   map[string]uint64
	namespaces map[string]NamespaceCoverage
	budget     int
}

type sensorState struct {
	name        string
	lastSeen    time.Time
	eventsTotal uint64
	windowStart time.Time
	windowCount uint64
}

// NewRegistry returns an empty coverage registry.
func NewRegistry() *Registry {
	return &Registry{
		sensors:    map[string]*sensorState{},
		unkBySvc:   map[string]uint64{},
		namespaces: map[string]NamespaceCoverage{},
		budget:     digest.DefaultAlertBudget,
	}
}

// SetAlertBudget overrides the soft daily target (default 5).
func (r *Registry) SetAlertBudget(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n > 0 {
		r.budget = n
	}
}

// ObserveIngest records a sensor sighting and attribution outcomes from a batch.
// An empty event list is a heartbeat (updates last-seen only).
func (r *Registry) ObserveIngest(sensor string, events []model.Attributed, now time.Time) {
	if sensor == "" {
		sensor = "unknown"
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	s := r.sensors[sensor]
	if s == nil {
		s = &sensorState{name: sensor, windowStart: now}
		r.sensors[sensor] = s
	}
	s.lastSeen = now
	if len(events) == 0 {
		return
	}
	s.eventsTotal += uint64(len(events))
	if now.Sub(s.windowStart) >= rateWindow {
		s.windowStart = now
		s.windowCount = 0
	}
	s.windowCount += uint64(len(events))

	for _, ev := range events {
		switch ev.Package {
		case "<unknown>":
			r.attrUnk++
			svc := ev.Service
			if svc == "" {
				svc = "<unnamed>"
			}
			r.unkBySvc[svc]++
		case "<app>":
			r.attrApp++
		case "":
			// skipped by fingerprint engine; ignore
		default:
			r.attrPkg++
		}
	}
}

// SetNamespaces replaces the namespace coverage rows reported by a sensor.
func (r *Registry) SetNamespaces(reportedBy string, rows []NamespaceCoverage, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	at := uint64(now.UnixNano())
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		row.Name = name
		row.ReportedBy = reportedBy
		row.ReportedAt = at
		if row.PodsWithout == 0 && row.PodsTotal > 0 {
			row.PodsWithout = row.PodsTotal - row.PodsWithNodeOptions
		}
		r.namespaces[name] = row
	}
}

// Snapshot builds the coverage panel payload.
func (r *Registry) Snapshot(now time.Time, alertsLast24h int) Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := Snapshot{
		Sensors:    []SensorHealth{},
		Namespaces: []NamespaceCoverage{},
		AlertBudget: AlertBudget{
			TargetPerDay:  r.budget,
			AlertsLast24h: alertsLast24h,
		},
		Attribution: AttributionKPI{
			Package:    r.attrPkg,
			App:        r.attrApp,
			Unknown:    r.attrUnk,
			TopUnknown: []UnknownService{},
		},
	}
	total := float64(r.attrPkg + r.attrApp + r.attrUnk)
	if total > 0 {
		out.Attribution.SuccessRate = float64(r.attrPkg) / total
	}

	for _, s := range r.sensors {
		st := "running"
		age := now.Sub(s.lastSeen)
		if age > sensorStaleAfter {
			st = "stale"
		}
		rate := 0.0
		elapsed := now.Sub(s.windowStart).Seconds()
		if elapsed > 0 && s.windowCount > 0 {
			rate = float64(s.windowCount) / elapsed
		}
		out.Sensors = append(out.Sensors, SensorHealth{
			Name:         s.name,
			Status:       st,
			LastSeen:     uint64(s.lastSeen.UnixNano()),
			EventsPerSec: rate,
			EventsTotal:  s.eventsTotal,
		})
	}
	sort.Slice(out.Sensors, func(i, j int) bool { return out.Sensors[i].Name < out.Sensors[j].Name })

	for svc, n := range r.unkBySvc {
		out.Attribution.TopUnknown = append(out.Attribution.TopUnknown, UnknownService{Service: svc, Count: n})
	}
	sort.Slice(out.Attribution.TopUnknown, func(i, j int) bool {
		if out.Attribution.TopUnknown[i].Count == out.Attribution.TopUnknown[j].Count {
			return out.Attribution.TopUnknown[i].Service < out.Attribution.TopUnknown[j].Service
		}
		return out.Attribution.TopUnknown[i].Count > out.Attribution.TopUnknown[j].Count
	})
	if len(out.Attribution.TopUnknown) > 10 {
		out.Attribution.TopUnknown = out.Attribution.TopUnknown[:10]
	}

	for _, ns := range r.namespaces {
		out.Namespaces = append(out.Namespaces, ns)
	}
	sort.Slice(out.Namespaces, func(i, j int) bool {
		// Gaps first: unlabeled or pods missing NODE_OPTIONS.
		gi := !out.Namespaces[i].InjectLabel || out.Namespaces[i].PodsWithout > 0
		gj := !out.Namespaces[j].InjectLabel || out.Namespaces[j].PodsWithout > 0
		if gi != gj {
			return gi
		}
		return out.Namespaces[i].Name < out.Namespaces[j].Name
	})
	return out
}
