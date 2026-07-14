package report

import "sort"

// Delta is the week-over-week change between two reachability reports.
// Surfaced on GET /v1/report and in the weekly digest.
type Delta struct {
	Executed           int      `json:"executed"`
	Declared           int      `json:"declared"`
	ReachableVulns     int      `json:"reachable_vulns"`
	NewExecuted        []string `json:"new_executed_packages,omitempty"`
	NewReachableVulns  []string `json:"new_reachable_vuln_ids,omitempty"`
	PreviousComputedAt uint64   `json:"previous_computed_at,omitempty"`
}

// ComputeDelta returns current − previous. An empty previous yields zeros and
// no "new" lists (there is nothing to diff against).
func ComputeDelta(current, previous Report, previousComputedAt uint64) Delta {
	d := Delta{
		Executed:           current.ExecutedCount - previous.ExecutedCount,
		Declared:           current.DeclaredCount - previous.DeclaredCount,
		ReachableVulns:     countExecutedVulnRows(current) - countExecutedVulnRows(previous),
		PreviousComputedAt: previousComputedAt,
		NewExecuted:        []string{},
		NewReachableVulns:  []string{},
	}
	if previousComputedAt == 0 && previous.DeclaredCount == 0 && previous.ExecutedCount == 0 && len(previous.Rows) == 0 {
		return Delta{}
	}

	prevExec := map[string]bool{}
	for _, r := range previous.Rows {
		if r.Executed {
			prevExec[r.Name+"@"+r.DeclaredVersion] = true
		}
	}
	for _, r := range current.Rows {
		if !r.Executed {
			continue
		}
		key := r.Name + "@" + r.DeclaredVersion
		if !prevExec[key] {
			d.NewExecuted = append(d.NewExecuted, key)
		}
	}
	sort.Strings(d.NewExecuted)

	prevVuln := map[string]bool{}
	for _, r := range previous.VulnRows {
		if !r.Executed {
			continue
		}
		for _, v := range r.Vulns {
			prevVuln[v.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, r := range current.VulnRows {
		if !r.Executed {
			continue
		}
		for _, v := range r.Vulns {
			if prevVuln[v.ID] || seen[v.ID] {
				continue
			}
			seen[v.ID] = true
			d.NewReachableVulns = append(d.NewReachableVulns, v.ID)
		}
	}
	sort.Strings(d.NewReachableVulns)
	return d
}

func countExecutedVulnRows(r Report) int {
	n := 0
	for _, row := range r.VulnRows {
		if row.Executed {
			n++
		}
	}
	return n
}
