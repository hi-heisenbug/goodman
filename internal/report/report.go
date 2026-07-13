package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hi-heisenbug/goodman/internal/model"
)

// PackageRow is one declared package annotated with whether Goodman observed
// it executing and any known vulnerabilities.
type PackageRow struct {
	Name            string          `json:"name"`
	DeclaredVersion string          `json:"declared_version"`
	Dev             bool            `json:"dev,omitempty"`
	Executed        bool            `json:"executed"`
	ExecutedVersion string          `json:"executed_version,omitempty"` // version Goodman actually observed (may differ)
	Behaviors       int             `json:"behaviors,omitempty"`        // distinct behaviors observed
	Vulns           []Vulnerability `json:"vulns,omitempty"`
}

// Report is the assembled reachability analysis.
type Report struct {
	Service       string       `json:"service,omitempty"`
	DeclaredCount int          `json:"declared_count"`
	ExecutedCount int          `json:"executed_count"`
	VulnRows      []PackageRow `json:"vuln_rows"` // rows with at least one vulnerability
	Rows          []PackageRow `json:"rows"`      // every declared package
}

// Build joins declared packages against observed fingerprints and (optional)
// vulnerabilities. fingerprints is what /v1/fingerprints returns; vulns is
// keyed by "name@version" ("" when OSV enrichment was skipped).
func Build(service string, declared []DeclaredPackage, fingerprints []model.Fingerprint, vulns map[string][]Vulnerability) Report {
	// Index observed packages by name -> (version, distinct behaviors).
	type observed struct {
		version   string
		behaviors int
	}
	byName := map[string]observed{}
	for _, fp := range fingerprints {
		if service != "" && fp.Service != service {
			continue
		}
		o := byName[fp.Package]
		if len(fp.Behaviors) >= o.behaviors {
			byName[fp.Package] = observed{version: fp.Version, behaviors: len(fp.Behaviors)}
		}
	}

	// Initialize as empty (not nil) slices so JSON serializes to [] rather
	// than null; API clients iterate these directly.
	rep := Report{Service: service, DeclaredCount: len(declared), VulnRows: []PackageRow{}, Rows: []PackageRow{}}
	for _, d := range declared {
		row := PackageRow{
			Name:            d.Name,
			DeclaredVersion: d.Version,
			Dev:             d.Dev,
			Vulns:           vulns[d.Name+"@"+d.Version],
		}
		if o, ok := byName[d.Name]; ok {
			row.Executed = true
			row.ExecutedVersion = o.version
			row.Behaviors = o.behaviors
			rep.ExecutedCount++
		}
		if len(row.Vulns) > 0 {
			rep.VulnRows = append(rep.VulnRows, row)
		}
		rep.Rows = append(rep.Rows, row)
	}

	// Vulnerable + executing packages first (they actually run), then
	// vulnerable-but-idle, then by name.
	sort.SliceStable(rep.VulnRows, func(i, j int) bool {
		if rep.VulnRows[i].Executed != rep.VulnRows[j].Executed {
			return rep.VulnRows[i].Executed
		}
		return rep.VulnRows[i].Name < rep.VulnRows[j].Name
	})
	return rep
}

// Markdown renders the report as a self-contained markdown document.
func (r Report) Markdown() string {
	var b strings.Builder
	title := "# Runtime reachability report"
	if r.Service != "" {
		title += ": " + r.Service
	}
	fmt.Fprintf(&b, "%s\n\n", title)

	idle := r.DeclaredCount - r.ExecutedCount
	pct := 0
	if r.DeclaredCount > 0 {
		pct = r.ExecutedCount * 100 / r.DeclaredCount
	}
	fmt.Fprintf(&b, "Goodman observed **%d of %d** declared packages actually executing (%d%%). ",
		r.ExecutedCount, r.DeclaredCount, pct)
	fmt.Fprintf(&b, "The remaining **%d** were shipped but never ran in production.\n\n", idle)

	execVuln, idleVuln := 0, 0
	for _, row := range r.VulnRows {
		if row.Executed {
			execVuln++
		} else {
			idleVuln++
		}
	}

	if len(r.VulnRows) == 0 {
		b.WriteString("## Vulnerabilities\n\nNo known vulnerabilities matched the declared packages")
		b.WriteString(" (or OSV enrichment was not run).\n\n")
	} else {
		fmt.Fprintf(&b, "## Vulnerabilities: prioritize the %d that execute\n\n", execVuln)
		b.WriteString("Vulnerabilities in packages Goodman observed running are listed first: ")
		b.WriteString("they are reachable at runtime, so they are the ones to patch now. ")
		fmt.Fprintf(&b, "%d more vulnerable packages were declared but never executed and can be deprioritized.\n\n", idleVuln)

		b.WriteString("| Package | Declared | Executed | Behaviors | Advisory | Severity |\n")
		b.WriteString("|---|---|---|---|---|---|\n")
		for _, row := range r.VulnRows {
			exec := "no"
			behaviors := "-"
			if row.Executed {
				exec = "**yes**"
				behaviors = fmt.Sprintf("%d", row.Behaviors)
			}
			for _, v := range row.Vulns {
				fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
					row.Name, row.DeclaredVersion, exec, behaviors, v.ID, v.Severity)
			}
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## Never executed (%d)\n\n", idle)
	b.WriteString("These declared packages produced no runtime behavior during the observed window. ")
	b.WriteString("Confirm before removing (some run only on rare code paths), but they are strong candidates for pruning.\n\n")
	n := 0
	for _, row := range r.Rows {
		if row.Executed {
			continue
		}
		dev := ""
		if row.Dev {
			dev = " (dev)"
		}
		fmt.Fprintf(&b, "- %s@%s%s\n", row.Name, row.DeclaredVersion, dev)
		if n++; n >= 200 {
			fmt.Fprintf(&b, "- ... and %d more\n", idle-n)
			break
		}
	}
	b.WriteString("\n")
	return b.String()
}
