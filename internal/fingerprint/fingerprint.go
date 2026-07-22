// Package fingerprint aggregates attributed events into per
// (service, package, version) behavior sets and promotes them to baselines
// after the learning window.
package fingerprint

import (
	"context"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/store"
)

// LearningWindow controls baseline promotion: a fingerprint becomes a
// baseline once it has enough observations AND enough wall-clock age
// (periodic jobs need wall-clock to show up).
type LearningWindow struct {
	MinObs int
	MinAge time.Duration
}

var DefaultLearningWindow = LearningWindow{MinObs: 500, MinAge: 24 * time.Hour}

type Engine struct {
	store  *store.Store
	window LearningWindow
}

func NewEngine(s *store.Store, w LearningWindow) *Engine {
	if w.MinObs <= 0 {
		w.MinObs = DefaultLearningWindow.MinObs
	}
	return &Engine{store: s, window: w}
}

// Update is the result of ingesting events for one fingerprint.
type Update struct {
	Fingerprint    *model.Fingerprint
	FreshBehaviors []string                    // behaviors first seen in this batch
	FreshEvents    map[string]model.Attributed // first event that introduced each fresh behavior
	JustPromoted   bool                        // crossed the learning window in this batch
}

type fingerprintKey struct {
	service string
	pkg     string
	version string
}

// Ingest merges a batch of events into fingerprints and returns one Update
// per touched (service, package, version) so the diff engine can react.
func (e *Engine) Ingest(ctx context.Context, events []model.Attributed) ([]Update, error) {
	grouped := groupEvents(events)
	updates := make([]Update, 0, len(grouped))
	for key, group := range grouped {
		update, err := e.ingestGroup(ctx, key, group)
		if err != nil {
			return nil, err
		}
		updates = append(updates, update)
	}
	return updates, nil
}

func groupEvents(events []model.Attributed) map[fingerprintKey][]model.Attributed {
	grouped := make(map[fingerprintKey][]model.Attributed)
	for _, event := range events {
		if event.Denied || event.Package == "" || event.Behavior == "" {
			continue
		}
		key := fingerprintKey{service: event.Service, pkg: event.Package, version: event.Version}
		grouped[key] = append(grouped[key], event)
	}
	return grouped
}

func (e *Engine) ingestGroup(ctx context.Context, key fingerprintKey, events []model.Attributed) (Update, error) {
	update := Update{FreshEvents: make(map[string]model.Attributed)}
	fingerprint, err := e.store.MergeFingerprint(ctx, key.service, key.pkg, key.version, func(fingerprint *model.Fingerprint) {
		update.FreshBehaviors, update.FreshEvents = mergeEvents(fingerprint, events)
		if !fingerprint.IsBaseline && e.qualifies(fingerprint) {
			fingerprint.IsBaseline = true
			update.JustPromoted = true
		}
	})
	update.Fingerprint = fingerprint
	return update, err
}

func mergeEvents(fingerprint *model.Fingerprint, events []model.Attributed) ([]string, map[string]model.Attributed) {
	if fingerprint.Behaviors == nil {
		fingerprint.Behaviors = make(map[string]model.BehaviorStat)
	}
	var fresh []string
	freshEvents := make(map[string]model.Attributed)
	for _, event := range events {
		stat, known := fingerprint.Behaviors[event.Behavior]
		if !known {
			stat.FirstSeen = event.Timestamp
			fresh = append(fresh, event.Behavior)
			freshEvents[event.Behavior] = event
		}
		stat.Count++
		stat.LastSeen = max(stat.LastSeen, event.Timestamp)
		fingerprint.Behaviors[event.Behavior] = stat
		fingerprint.ObsCount++
		fingerprint.LastSeen = max(fingerprint.LastSeen, event.Timestamp)
		if fingerprint.FirstSeen == 0 || event.Timestamp < fingerprint.FirstSeen {
			fingerprint.FirstSeen = event.Timestamp
		}
	}
	return fresh, freshEvents
}

func (e *Engine) qualifies(fp *model.Fingerprint) bool {
	age := time.Duration(fp.LastSeen-fp.FirstSeen) * time.Nanosecond
	return fp.ObsCount >= e.window.MinObs && age >= e.window.MinAge
}
