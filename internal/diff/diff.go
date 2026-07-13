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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/store"
)

var wouldBlockTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "goodman_enforce_would_block_total",
	Help: "Alerts that matched a rule with action=warn (audit would-block)."},
	[]string{"rule"})

// Rule marks matching new behaviors as high risk (CRITICAL).
//
// AlwaysOn rules fire even when no baseline exists yet: some behaviors
// (credential reads, cloud-metadata access) are alert-worthy the first time
// they are ever seen, during the learning window included. Rules without
// AlwaysOn only escalate drift against an established baseline.
//
// Exclude patterns suppress a match without deleting the rule, so operators
// can tune noise ("new outbound connects are critical, except to our CDN").
//
// Action controls enforcement posture without changing detection:
//
//	"alert" (default) — normal alert only
//	"warn"            — alert plus WouldBlock (audit: would have been blocked)
//	"block"           — alert plus WouldBlock; kernel denies when enforcement armed
type Rule struct {
	Name     string   `json:"name"`
	Pattern  string   `json:"pattern"`             // regex matched against the behavior string
	AlwaysOn bool     `json:"always_on,omitempty"` // fire without a baseline (learning window included)
	Exclude  []string `json:"exclude,omitempty"`   // regexes that suppress a match
	Action   string   `json:"action,omitempty"`    // "alert" (default) | "warn" | "block"
	re       *regexp.Regexp
	exclude  []*regexp.Regexp
}

// Rule action values. Empty Action on disk is normalized to ActionAlert.
const (
	ActionAlert = "alert"
	ActionWarn  = "warn"
	ActionBlock = "block"
)

// DefaultRules encode the four attack patterns from the May-2026 incidents:
// secret reads, cloud-metadata access, new outbound connects, new execs.
// Secret reads and cloud-metadata access are always-on: they are never
// legitimate "learning", so they alert from minute one. Connects and execs
// are drift-only; almost every package does one legitimately.
var DefaultRules = []Rule{
	{Name: "secret-read", AlwaysOn: true, Pattern: `^READ .*(secret|token|credential|password|shadow|wallet|\.pem|\.key|\.aws|\.ssh|\.npmrc|\.env|id_rsa)`},
	{Name: "cloud-metadata", AlwaysOn: true, Pattern: `^CONNECT 169\.254\.169\.254:`},
	{Name: "new-outbound-connect", Pattern: `^CONNECT `},
	{Name: "new-exec", Pattern: `^EXEC `},
}

// LoadRules reads a JSON rule file; falls back to DefaultRules when path is
// empty. Invalid patterns or unknown actions are an error — a silently
// dropped rule is a silently missed CRITICAL.
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
	return CompileRules(rules)
}

// CompileRules validates and compiles an in-memory rule list (same rules as
// LoadRules). Used by tests and the replay corpus when a scenario ships its
// own rules.
func CompileRules(rules []Rule) ([]Rule, error) {
	out := make([]Rule, len(rules))
	for i, r := range rules {
		action := r.Action
		if action == "" {
			action = ActionAlert
		}
		switch action {
		case ActionAlert, ActionWarn, ActionBlock:
		default:
			return nil, fmt.Errorf("rule %q: unknown action %q (want %q, %q, or %q)", r.Name, r.Action, ActionAlert, ActionWarn, ActionBlock)
		}
		re, err := regexp.Compile("(?i)" + r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", r.Name, err)
		}
		excl := make([]*regexp.Regexp, len(r.Exclude))
		for j, e := range r.Exclude {
			if excl[j], err = regexp.Compile("(?i)" + e); err != nil {
				return nil, fmt.Errorf("rule %q exclude %q: %w", r.Name, e, err)
			}
		}
		out[i] = Rule{
			Name: r.Name, Pattern: r.Pattern, AlwaysOn: r.AlwaysOn,
			Exclude: r.Exclude, Action: action, re: re, exclude: excl,
		}
	}
	return out, nil
}

// Matches reports whether the behavior triggers this rule.
func (r *Rule) Matches(behavior string) bool { return r.matches(behavior) }

// matches reports whether the behavior triggers this rule: the pattern must
// match and no exclude pattern may match.
func (r *Rule) matches(behavior string) bool {
	if !r.re.MatchString(behavior) {
		return false
	}
	for _, e := range r.exclude {
		if e.MatchString(behavior) {
			return false
		}
	}
	return true
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
// Three triggers:
//  1. A new version is accumulating behavior while an older version of the
//     same (service, package) already has a baseline -> version-drift.
//  2. New behavior appears on a version AFTER it was promoted to baseline
//     -> same-version drift.
//  0. No baseline exists at all, but a fresh behavior matches an always-on
//     high-risk rule -> alert during the learning window. Without this, a
//     package that is malicious from day one is silently baselined
//     (poisoning), and the product is mute for the whole learning window.
func (eng *Engine) React(ctx context.Context, up fingerprint.Update) (*model.Alert, error) {
	fp := up.Fingerprint

	// Trigger 2: baseline version doing something it never did before.
	// (JustPromoted means the fresh behaviors were learned, not drift.)
	if fp.IsBaseline && !up.JustPromoted && len(up.FreshBehaviors) > 0 {
		return eng.emit(ctx, fp.Service, fp.Package, fp.Version, fp.Version,
			baselineBehaviors(fp.Behaviors, up.FreshBehaviors), up.FreshBehaviors, up.FreshEvents)
	}

	// Trigger 1: new (not yet baseline) version vs previous version's baseline.
	if !fp.IsBaseline {
		base, err := eng.store.LatestBaseline(ctx, fp.Service, fp.Package, fp.Version)
		if err != nil {
			return nil, err
		}
		if base == nil {
			// Trigger 0: still learning, no baseline anywhere. Alert only on
			// always-on high-risk behaviors; everything else is learning.
			var hot []string
			for _, b := range up.FreshBehaviors {
				for i := range eng.rules {
					if eng.rules[i].AlwaysOn && eng.rules[i].matches(b) {
						hot = append(hot, b)
						break
					}
				}
			}
			if len(hot) == 0 {
				return nil, nil
			}
			return eng.emit(ctx, fp.Service, fp.Package, "", fp.Version,
				baselineBehaviors(fp.Behaviors, hot), hot, up.FreshEvents)
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
			behaviorKeys(base.Behaviors), novel, up.FreshEvents)
	}
	return nil, nil
}

func (eng *Engine) emit(ctx context.Context, service, pkg, oldV, newV string, baselineBehaviors, newBehaviors []string, freshEvents map[string]model.Attributed) (*model.Alert, error) {
	severity := model.SeverityWarn
	ruleSet := map[string]bool{}
	warnRules := map[string]bool{}
	evidence := make([]model.Evidence, 0, len(newBehaviors))
	for _, b := range newBehaviors {
		ev := model.Evidence{Behavior: b}
		for i := range eng.rules {
			if eng.rules[i].matches(b) {
				severity = model.SeverityCritical
				ruleSet[eng.rules[i].Name] = true
				ev.Rules = append(ev.Rules, eng.rules[i].Name)
				if eng.rules[i].Action == ActionWarn || eng.rules[i].Action == ActionBlock {
					warnRules[eng.rules[i].Name] = true
				}
			}
		}
		if first, ok := freshEvents[b]; ok {
			ev.Sensor = first.Sensor
			ev.FirstSeen = first.Timestamp
		}
		evidence = append(evidence, ev)
	}
	matched := make([]string, 0, len(ruleSet))
	for name := range ruleSet {
		matched = append(matched, name)
	}
	sort.Strings(matched)

	a := &model.Alert{
		ID:                alertID(service, pkg, oldV, newV),
		Service:           service,
		Package:           pkg,
		OldVersion:        oldV,
		NewVersion:        newV,
		Severity:          severity,
		BaselineBehaviors: baselineBehaviors,
		NewBehaviors:      newBehaviors,
		MatchedRules:      matched,
		WouldBlock:        len(warnRules) > 0,
		Evidence:          evidence,
		DetectedAt:        uint64(time.Now().UnixNano()),
		Status:            model.AlertOpen,
	}
	if _, err := eng.store.UpsertAlert(ctx, a); err != nil {
		return nil, err
	}
	for name := range warnRules {
		wouldBlockTotal.WithLabelValues(name).Inc()
	}
	return a, nil
}

// ReactDenied upgrades an existing alert when a kernel deny event arrives for
// a behavior that already triggered an alert. Returns the updated alert or nil.
func (eng *Engine) ReactDenied(ctx context.Context, ev model.Attributed) (*model.Alert, error) {
	if !ev.Denied || ev.Behavior == "" || ev.Package == "" {
		return nil, nil
	}
	alerts, err := eng.store.ListAlerts(ctx, "")
	if err != nil {
		return nil, err
	}
	for i := range alerts {
		a := &alerts[i]
		for _, b := range a.NewBehaviors {
			if b != ev.Behavior {
				continue
			}
			if a.Blocked {
				return nil, nil
			}
			a.Blocked = true
			if err := eng.store.SetAlertBlocked(ctx, a.ID, true); err != nil {
				return nil, err
			}
			return a, nil
		}
	}
	return nil, nil
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
