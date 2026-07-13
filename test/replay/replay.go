// Package replay runs Goodman against reproductions of real npm
// supply-chain attacks. Each scenario is a benign, self-contained fixture:
// a baseline the collector learns, then a "malicious" version whose new
// behavior must raise exactly the expected CRITICAL alert. The corpus is
// simultaneously a regression suite, a live demo (`make replay`), and the
// answer to "would Goodman have caught <attack>?".
package replay

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"

	"github.com/hi-heisenbug/goodman/internal/diff"
	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/store"
)

//go:embed scenarios/*.json
var scenarioFS embed.FS

// Scenario is one reproduced attack. When Baseline is nil the malicious
// behavior must be caught with no baseline at all (the always-on rule path).
// Rules, when non-empty, replace the built-in defaults for that scenario
// (used to exercise enforce=warn without changing production defaults).
type Scenario struct {
	Name      string        `json:"name"`
	Incident  string        `json:"incident"`
	Reference string        `json:"reference"`
	Summary   string        `json:"summary"`
	Service   string        `json:"service"`
	Package   string        `json:"package"`
	Rules     []diff.Rule   `json:"rules,omitempty"`
	Baseline  *VersionState `json:"baseline"`
	Malicious VersionState  `json:"malicious"`
	Expect    Expectation   `json:"expect"`
}

type VersionState struct {
	Version   string   `json:"version"`
	Behaviors []string `json:"behaviors"`
}

type Expectation struct {
	Severity     string   `json:"severity"`
	OldVersion   string   `json:"old_version"`
	NewVersion   string   `json:"new_version"`
	NewBehaviors []string `json:"new_behaviors"`
	MatchedRules []string `json:"matched_rules"`
	WouldBlock   bool     `json:"would_block,omitempty"`
}

// LoadScenarios reads every embedded scenario, sorted by name.
func LoadScenarios() ([]Scenario, error) {
	entries, err := scenarioFS.ReadDir("scenarios")
	if err != nil {
		return nil, err
	}
	var out []Scenario
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := scenarioFS.ReadFile("scenarios/" + e.Name())
		if err != nil {
			return nil, err
		}
		var s Scenario
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Run drives one scenario through a fresh in-memory pipeline and returns the
// alert it produced (nil if none). A tiny learning window promotes the
// baseline immediately so replays are fast and deterministic.
func Run(ctx context.Context, s Scenario, dbPath string) (*model.Alert, error) {
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer st.Close()

	rules, err := diff.LoadRules("")
	if err != nil {
		return nil, err
	}
	if len(s.Rules) > 0 {
		rules, err = diff.CompileRules(s.Rules)
		if err != nil {
			return nil, err
		}
	}
	// A high learning window so the malicious version, seen only briefly,
	// never auto-promotes: it must be diffed as live drift, not learned as a
	// new baseline. The baseline version is promoted explicitly below, which
	// mirrors production where the baseline was learned over days.
	fpEng := fingerprint.NewEngine(st, fingerprint.LearningWindow{MinObs: 1_000_000, MinAge: 1})
	diffEng := diff.NewEngine(st, rules)

	ts := uint64(1)
	feed := func(version string, behaviors []string) (*model.Alert, error) {
		var evs []model.Attributed
		for _, b := range behaviors {
			ts++
			evs = append(evs, model.Attributed{
				Service: s.Service, Package: s.Package, Version: version,
				Behavior: b, Timestamp: ts, Sensor: "replay-" + s.Name,
			})
		}
		ups, err := fpEng.Ingest(ctx, evs)
		if err != nil {
			return nil, err
		}
		var last *model.Alert
		for _, up := range ups {
			a, err := diffEng.React(ctx, up)
			if err != nil {
				return nil, err
			}
			if a != nil {
				last = a
			}
		}
		return last, nil
	}

	if s.Baseline != nil {
		if _, err := feed(s.Baseline.Version, s.Baseline.Behaviors); err != nil {
			return nil, err
		}
		// Promote the baseline the way the learning window eventually would,
		// so the malicious version diffs against an established baseline.
		fp, err := st.GetFingerprint(ctx, s.Service, s.Package, s.Baseline.Version)
		if err != nil {
			return nil, err
		}
		if fp == nil {
			return nil, fmt.Errorf("%s: baseline fingerprint not stored", s.Name)
		}
		fp.IsBaseline = true
		if err := st.UpsertFingerprint(ctx, fp); err != nil {
			return nil, err
		}
	}
	return feed(s.Malicious.Version, s.Malicious.Behaviors)
}
