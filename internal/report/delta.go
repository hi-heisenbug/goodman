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
	if isEmptyReport(previous, previousComputedAt) {
		return Delta{}
	}
	d := Delta{
		Executed:           current.ExecutedCount - previous.ExecutedCount,
		Declared:           current.DeclaredCount - previous.DeclaredCount,
		ReachableVulns:     countExecutedVulnRows(current) - countExecutedVulnRows(previous),
		PreviousComputedAt: previousComputedAt,
		NewExecuted:        newExecutedPackages(current.Rows, previous.Rows),
		NewReachableVulns:  newReachableVulnerabilityIDs(current.VulnRows, previous.VulnRows),
	}
	return d
}

func isEmptyReport(report Report, computedAt uint64) bool {
	return computedAt == 0 && report.DeclaredCount == 0 && report.ExecutedCount == 0 && len(report.Rows) == 0
}

func newExecutedPackages(current, previous []PackageRow) []string {
	known := make(map[string]bool)
	for _, row := range previous {
		if row.Executed {
			known[packageVersion(row)] = true
		}
	}
	var added []string
	for _, row := range current {
		key := packageVersion(row)
		if row.Executed && !known[key] {
			added = append(added, key)
		}
	}
	sort.Strings(added)
	if added == nil {
		return []string{}
	}
	return added
}

func packageVersion(row PackageRow) string {
	return row.Name + "@" + row.DeclaredVersion
}

func newReachableVulnerabilityIDs(current, previous []PackageRow) []string {
	known := reachableVulnerabilityIDs(previous)
	seen := make(map[string]bool)
	var added []string
	for _, row := range current {
		if !row.Executed {
			continue
		}
		for _, vulnerability := range row.Vulns {
			if known[vulnerability.ID] || seen[vulnerability.ID] {
				continue
			}
			seen[vulnerability.ID] = true
			added = append(added, vulnerability.ID)
		}
	}
	sort.Strings(added)
	if added == nil {
		return []string{}
	}
	return added
}

func reachableVulnerabilityIDs(rows []PackageRow) map[string]bool {
	ids := make(map[string]bool)
	for _, row := range rows {
		if !row.Executed {
			continue
		}
		for _, vulnerability := range row.Vulns {
			ids[vulnerability.ID] = true
		}
	}
	return ids
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
