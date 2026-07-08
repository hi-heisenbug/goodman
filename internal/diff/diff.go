// Package diff compares live behavior against baselines and emits drift
// alerts. Severity is decided by a config-driven high-risk rule list, not
// hard-coded ifs, so customers can tune it.
package diff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/store"
)

// Rule marks matching new behaviors as high risk (CRITICAL).
type Rule struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"` // regex matched against the behavior string
	re      *regexp.Regexp
}

// DefaultRules encode the four attack patterns from the May-2026 incidents:
// secret reads, cloud-metadata access, new outbound connects, new execs.
var DefaultRules = []Rule{
	{Name: "secret-read", Pattern: `^READ .*(secret|token|credential|password|shadow|\.pem|\.key|\.aws|\.ssh|\.npmrc|\.env|id_rsa)`},
	{Name: "cloud-metadata", Pattern: `^CONNECT 169\.254\.169\.254:`},
	{Name: "new-outbound-connect", Pattern: `^CONNECT `},
	{Name: "new-exec", Pattern: `^EXEC `},
}

// LoadRules reads a JSON rule file; falls back to DefaultRules when path is
// empty. Invalid patterns are an error — a silently dropped rule is a
// silently missed CRITICAL.
func LoadRules(path string) ([]Rule, error) {
	rules := DefaultRules
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read rules %s: %w", path, err)
		}
		rules = nil
		if err := json.Unmarshal(data, &rules); err != nil {
			return nil, fmt.Errorf("parse rules %s: %w", path, err)
		}
	}
	out := make([]Rule, len(rules))
	for i, r := range rules {
		re, err := regexp.Compile("(?i)" + r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", r.Name, err)
		}
		out[i] = Rule{Name: r.Name, Pattern: r.Pattern, re: re}
	}
	return out, nil
}

type Engine struct {
	store *store.Store
	rules []Rule
}

func NewEngine(s *store.Store, rules []Rule) *Engine {
	return &Engine{store: s, rules: rules}
}

// alertID is deterministic so repeated drift on the same version transition
// merges into one alert instead of flooding.
func alertID(service, pkg, oldV, newV string) string {
	h := sha256.Sum256([]byte(service + "|" + pkg + "|" + oldV + "|" + newV))
	return hex.EncodeToString(h[:12])
}

// React inspects one fingerprint update and emits/updates an alert when the
// live behavior set has drifted from the relevant baseline. Returns the
// alert if one was created or extended.
//
// Two triggers (per plan §9):
//  1. A new version is accumulating behavior while an older version of the
//     same (service, package) already has a baseline -> version-drift.
//  2. New behavior appears on a version AFTER it was promoted to baseline
//     -> same-version drift.
func (eng *Engine) React(ctx context.Context, up fingerprint.Update) (*model.Alert, error) {
	fp := up.Fingerprint

	// Trigger 2: baseline version doing something it never did before.
	// (JustPromoted means the fresh behaviors were learned, not drift.)
	if fp.IsBaseline && !up.JustPromoted && len(up.FreshBehaviors) > 0 {
		return eng.emit(ctx, fp.Service, fp.Package, fp.Version, fp.Version,
			baselineBehaviors(fp.Behaviors, up.FreshBehaviors), up.FreshBehaviors)
	}

	// Trigger 1: new (not yet baseline) version vs previous version's baseline.
	if !fp.IsBaseline {
		base, err := eng.store.LatestBaseline(ctx, fp.Service, fp.Package, fp.Version)
		if err != nil || base == nil {
			return nil, err // no baseline yet -> still learning, never alert
		}
		var novel []string
		for _, b := range up.FreshBehaviors {
			if _, inBaseline := base.Behaviors[b]; !inBaseline {
				novel = append(novel, b)
			}
		}
		if len(novel) == 0 {
			return nil, nil // version bump with identical behavior: NO alert
		}
		return eng.emit(ctx, fp.Service, fp.Package, base.Version, fp.Version,
			behaviorKeys(base.Behaviors), novel)
	}
	return nil, nil
}

func (eng *Engine) emit(ctx context.Context, service, pkg, oldV, newV string, baselineBehaviors, newBehaviors []string) (*model.Alert, error) {
	severity := model.SeverityWarn
	for _, b := range newBehaviors {
		for _, r := range eng.rules {
			if r.re.MatchString(b) {
				severity = model.SeverityCritical
			}
		}
	}
	a := &model.Alert{
		ID:                alertID(service, pkg, oldV, newV),
		Service:           service,
		Package:           pkg,
		OldVersion:        oldV,
		NewVersion:        newV,
		Severity:          severity,
		BaselineBehaviors: baselineBehaviors,
		NewBehaviors:      newBehaviors,
		DetectedAt:        uint64(time.Now().UnixNano()),
		Status:            model.AlertOpen,
	}
	if _, err := eng.store.UpsertAlert(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func behaviorKeys(behaviors map[string]model.BehaviorStat) []string {
	keys := make([]string, 0, len(behaviors))
	for b := range behaviors {
		keys = append(keys, b)
	}
	sort.Strings(keys)
	return keys
}

func baselineBehaviors(behaviors map[string]model.BehaviorStat, fresh []string) []string {
	skip := make(map[string]bool, len(fresh))
	for _, b := range fresh {
		skip[b] = true
	}
	keys := make([]string, 0, len(behaviors))
	for b := range behaviors {
		if !skip[b] {
			keys = append(keys, b)
		}
	}
	sort.Strings(keys)
	return keys
}
