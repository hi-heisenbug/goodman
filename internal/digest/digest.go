// Package digest builds the weekly pilot heartbeat: alert volume vs budget,
// reachability coverage and week-over-week deltas, delivered via the existing
// webhook layer so a silent 30-day POV still speaks on a schedule.
package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/report"
	"github.com/hi-heisenbug/goodman/internal/store"
)

// DefaultAlertBudget is the soft noise target a security lead uses to judge
// whether the DaemonSet is worth keeping (Phase 9 surfaces the same number).
const DefaultAlertBudget = 5

// Digest is one weekly heartbeat snapshot.
type Digest struct {
	GeneratedAt      time.Time    `json:"generated_at"`
	OpenAlerts       int          `json:"open_alerts"`
	AlertBudget      int          `json:"alert_budget"`
	HasLockfile      bool         `json:"has_lockfile"`
	DeclaredCount    int          `json:"declared_count"`
	ExecutedCount    int          `json:"executed_count"`
	ReachableVulns   int          `json:"reachable_vulns"`
	Delta            report.Delta `json:"delta"`
	NewExecuted      []string     `json:"new_executed_packages"`
	NewReachableCVEs []string     `json:"new_reachable_cves"`
	Services         []string     `json:"services"`
	PublicURL        string       `json:"public_url,omitempty"`
}

// Build assembles a digest from the store. service "" is the all-scope demo
// / default report; when that is missing the first available service is used.
func Build(ctx context.Context, st *store.Store, alertBudget int, publicURL string) (Digest, error) {
	if alertBudget <= 0 {
		alertBudget = DefaultAlertBudget
	}
	d := Digest{
		GeneratedAt:      time.Now().UTC(),
		AlertBudget:      alertBudget,
		PublicURL:        strings.TrimRight(strings.TrimSpace(publicURL), "/"),
		NewExecuted:      []string{},
		NewReachableCVEs: []string{},
		Services:         []string{},
	}

	alerts, err := st.ListAlerts(ctx, model.AlertOpen)
	if err != nil {
		return d, err
	}
	d.OpenAlerts = len(alerts)

	lfs, err := st.ListLockfiles(ctx)
	if err != nil {
		return d, err
	}
	for _, lf := range lfs {
		d.Services = append(d.Services, lf.Service)
	}
	if len(lfs) == 0 {
		return d, nil
	}
	d.HasLockfile = true

	service := pickService(lfs)
	stored, found, err := st.GetReport(ctx, service)
	if err != nil {
		return d, err
	}
	if !found {
		return d, nil
	}
	var cur report.Report
	if err := json.Unmarshal([]byte(stored.Report), &cur); err != nil {
		return d, fmt.Errorf("decode current report: %w", err)
	}
	d.DeclaredCount = cur.DeclaredCount
	d.ExecutedCount = cur.ExecutedCount
	for _, row := range cur.VulnRows {
		if row.Executed {
			d.ReachableVulns++
		}
	}

	var prev report.Report
	if stored.PreviousReport != "" {
		_ = json.Unmarshal([]byte(stored.PreviousReport), &prev)
	}
	d.Delta = report.ComputeDelta(cur, prev, stored.PreviousComputedAt)
	d.NewExecuted = append([]string{}, d.Delta.NewExecuted...)
	d.NewReachableCVEs = append([]string{}, d.Delta.NewReachableVulns...)
	return d, nil
}

func pickService(lfs []store.StoredLockfile) string {
	for _, lf := range lfs {
		if lf.Service == "" {
			return ""
		}
	}
	return lfs[0].Service
}

// Markdown renders the digest for operators who prefer a file or generic hook.
func (d Digest) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Goodman weekly digest\n\n")
	fmt.Fprintf(&b, "Generated %s\n\n", d.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "## Alerts\n\n")
	fmt.Fprintf(&b, "- Open alerts: **%d** (budget < %d/day target)\n", d.OpenAlerts, d.AlertBudget)
	if d.OpenAlerts > d.AlertBudget {
		fmt.Fprintf(&b, "- Above noise budget — tune rule excludes or CIDR aggregation.\n")
	}
	fmt.Fprintf(&b, "\n## Reachability\n\n")
	if !d.HasLockfile {
		b.WriteString("- No lockfile uploaded yet. Upload one in the Reachability tab to unlock coverage stats.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "- Declared: **%d** · Executed: **%d**", d.DeclaredCount, d.ExecutedCount)
	if d.Delta.PreviousComputedAt > 0 {
		fmt.Fprintf(&b, " (%+d executed vs prior)", d.Delta.Executed)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- Reachable vulnerabilities: **%d**", d.ReachableVulns)
	if d.Delta.PreviousComputedAt > 0 {
		fmt.Fprintf(&b, " (%+d vs prior)", d.Delta.ReachableVulns)
	}
	b.WriteString("\n")
	if len(d.NewExecuted) > 0 {
		fmt.Fprintf(&b, "- Newly executed: %s\n", strings.Join(clip(d.NewExecuted, 10), ", "))
	}
	if len(d.NewReachableCVEs) > 0 {
		fmt.Fprintf(&b, "- New reachable CVEs: %s\n", strings.Join(clip(d.NewReachableCVEs, 10), ", "))
	}
	if d.PublicURL != "" {
		fmt.Fprintf(&b, "\n[Open dashboard](%s/#reachability)\n", d.PublicURL)
	}
	return b.String()
}

// SlackPayload is a Slack-compatible incoming-webhook body.
func (d Digest) SlackPayload() map[string]any {
	return map[string]any{"text": d.slackText()}
}

// GenericPayload is the typed JSON body for non-Slack webhooks.
func (d Digest) GenericPayload() map[string]any {
	return map[string]any{"type": "goodman.digest", "digest": d}
}

func (d Digest) slackText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "*Goodman weekly digest* · %s\n", d.GeneratedAt.Format("2006-01-02"))
	fmt.Fprintf(&b, "Open alerts: *%d* (budget <%d)\n", d.OpenAlerts, d.AlertBudget)
	if !d.HasLockfile {
		b.WriteString("Reachability: no lockfile yet — upload one to unlock coverage.\n")
	} else {
		fmt.Fprintf(&b, "Reachability: %d declared / %d executed", d.DeclaredCount, d.ExecutedCount)
		if d.Delta.PreviousComputedAt > 0 {
			fmt.Fprintf(&b, " (%+d executed)", d.Delta.Executed)
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "Reachable vulns: %d", d.ReachableVulns)
		if d.Delta.PreviousComputedAt > 0 {
			fmt.Fprintf(&b, " (%+d)", d.Delta.ReachableVulns)
		}
		b.WriteString("\n")
		if len(d.NewExecuted) > 0 {
			fmt.Fprintf(&b, "Newly executed: `%s`\n", strings.Join(clip(d.NewExecuted, 5), "`, `"))
		}
		if len(d.NewReachableCVEs) > 0 {
			fmt.Fprintf(&b, "New reachable CVEs: `%s`\n", strings.Join(clip(d.NewReachableCVEs, 5), "`, `"))
		}
	}
	if d.PublicURL != "" {
		fmt.Fprintf(&b, "<%s/#reachability|Open Reachability>", d.PublicURL)
	}
	return b.String()
}

func clip(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	out := append([]string{}, in[:n]...)
	out = append(out, fmt.Sprintf("+%d more", len(in)-n))
	return out
}
