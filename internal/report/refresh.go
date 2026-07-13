package report

import (
	"context"
	"encoding/json"
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
	for _, lf := range lockfiles {
		declared, err := ParseLockfile([]byte(lf.Content))
		if err != nil {
			return n, fmt.Errorf("service %q lockfile: %w", lf.Service, err)
		}
		fps, err := st.ListFingerprints(ctx, lf.Service, "")
		if err != nil {
			return n, err
		}
		var vulns map[string][]Vulnerability
		if osvClient != nil {
			if v, err := osvClient.Query(ctx, declared); err == nil {
				vulns = v
			}
		}
		rep := Build(lf.Service, declared, fps, vulns)
		repJSON, err := json.Marshal(rep)
		if err != nil {
			return n, err
		}
		if err := st.SaveReport(ctx, lf.Service, string(repJSON), osvClient != nil, nowNs); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
