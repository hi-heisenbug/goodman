package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hi-heisenbug/goodman/internal/model"
)

// RefreshStore is the persistence RefreshAll writes through: the fingerprints
// to join against and where to store the resulting snapshots.
// internal/store.Store satisfies it.
type RefreshStore interface {
	ListFingerprints(ctx context.Context, service, pkg string) ([]model.Fingerprint, error)
	SaveReport(ctx context.Context, service, reportJSON string, osv bool, computedAt uint64) error
}

// Lockfile is one stored package-lock.json to recompute from (mirrors
// store.StoredLockfile; kept local so this package stays store-agnostic).
type Lockfile struct {
	Service string
	Content string
}

// RefreshAll recomputes and persists the reachability snapshot for each given
// lockfile, joining against the latest fingerprints. When osvClient is
// non-nil, snapshots are enriched with OSV advisories (an OSV failure degrades
// that service to a reachability-only snapshot rather than aborting the run).
// nowNs stamps the snapshots. Returns the number of services refreshed.
func RefreshAll(ctx context.Context, st RefreshStore, lockfiles []Lockfile, osvClient *OSVClient, nowNs uint64) (int, error) {
	n := 0
	var refreshErrors []error
	for _, lf := range lockfiles {
		declared, err := ParseLockfile([]byte(lf.Content))
		if err != nil {
			refreshErrors = append(refreshErrors, fmt.Errorf("service %q lockfile: %w", lf.Service, err))
			continue
		}
		fps, err := st.ListFingerprints(ctx, lf.Service, "")
		if err != nil {
			refreshErrors = append(refreshErrors, fmt.Errorf("service %q fingerprints: %w", lf.Service, err))
			continue
		}
		var vulns map[string][]Vulnerability
		osvComplete := false
		if osvClient != nil {
			if v, err := osvClient.Query(ctx, declared); err != nil {
				refreshErrors = append(refreshErrors, fmt.Errorf("service %q osv: %w", lf.Service, err))
			} else {
				vulns = v
				osvComplete = true
			}
		}
		rep := Build(lf.Service, declared, fps, vulns)
		repJSON, err := json.Marshal(rep)
		if err != nil {
			refreshErrors = append(refreshErrors, fmt.Errorf("service %q encode report: %w", lf.Service, err))
			continue
		}
		if err := st.SaveReport(ctx, lf.Service, string(repJSON), osvComplete, nowNs); err != nil {
			refreshErrors = append(refreshErrors, fmt.Errorf("service %q save report: %w", lf.Service, err))
			continue
		}
		n++
	}
	return n, errors.Join(refreshErrors...)
}
