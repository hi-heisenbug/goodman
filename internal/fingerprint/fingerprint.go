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

// Ingest merges a batch of events into fingerprints and returns one Update
// per touched (service, package, version) so the diff engine can react.
func (e *Engine) Ingest(ctx context.Context, events []model.Attributed) ([]Update, error) {
	type key struct{ service, pkg, version string }
	grouped := map[key][]model.Attributed{}
	for _, ev := range events {
		if ev.Package == "" || ev.Behavior == "" {
			continue
		}
		k := key{ev.Service, ev.Package, ev.Version}
		grouped[k] = append(grouped[k], ev)
	}

	var updates []Update
	for k, evs := range grouped {
		var fresh []string
		freshEvents := map[string]model.Attributed{}
		promoted := false

		fp, err := e.store.MergeFingerprint(ctx, k.service, k.pkg, k.version, func(fp *model.Fingerprint) {
			if fp.Behaviors == nil {
				fp.Behaviors = map[string]model.BehaviorStat{}
			}
			for _, ev := range evs {
				st, known := fp.Behaviors[ev.Behavior]
				if !known {
					st = model.BehaviorStat{FirstSeen: ev.Timestamp}
					fresh = append(fresh, ev.Behavior)
					freshEvents[ev.Behavior] = ev
				}
				st.Count++
				if ev.Timestamp > st.LastSeen {
					st.LastSeen = ev.Timestamp
				}
				fp.Behaviors[ev.Behavior] = st
				fp.ObsCount++
				if ev.Timestamp > fp.LastSeen {
					fp.LastSeen = ev.Timestamp
				}
				if fp.FirstSeen == 0 || ev.Timestamp < fp.FirstSeen {
					fp.FirstSeen = ev.Timestamp
				}
			}
			if !fp.IsBaseline && e.qualifies(fp) {
				fp.IsBaseline = true
				promoted = true
			}
		})
		if err != nil {
			return nil, err
		}
		updates = append(updates, Update{Fingerprint: fp, FreshBehaviors: fresh, FreshEvents: freshEvents, JustPromoted: promoted})
	}
	return updates, nil
}

func (e *Engine) qualifies(fp *model.Fingerprint) bool {
	age := time.Duration(fp.LastSeen-fp.FirstSeen) * time.Nanosecond
	return fp.ObsCount >= e.window.MinObs && age >= e.window.MinAge
}
