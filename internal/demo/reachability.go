package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/report"
)

// SeedReachability injects ExecutedCount fingerprints that match the first
// N entries of a DeclaredCount lockfile, then persists the reachability
// report so the dashboard shows 1,400 / 240 on first load.
func SeedReachability(ctx context.Context, c *Client) (*report.Report, error) {
	const service = "demo-app"
	base := uint64(time.Now().UnixNano())

	events := make([]model.Attributed, 0, ExecutedCount*3)
	for round := 0; round < 3; round++ {
		for i := 1; i <= ExecutedCount; i++ {
			name := PackageName(i)
			events = append(events, attr(
				service, name, PackageVersion(i),
				fmt.Sprintf("READ /app/node_modules/%s/**", name),
				base+uint64(round)*600_000_000+uint64(i)*1_000_000,
			))
		}
	}
	// Batch in chunks so a single POST stays modest.
	const chunk = 100
	for i := 0; i < len(events); i += chunk {
		end := i + chunk
		if end > len(events) {
			end = len(events)
		}
		if err := c.PostEvents(ctx, events[i:end]); err != nil {
			return nil, fmt.Errorf("seed reachability events: %w", err)
		}
	}

	lockfile := GenerateLockfile(DeclaredCount)
	rep, err := c.PostReport(ctx, lockfile, "", true, true)
	if err != nil {
		return nil, fmt.Errorf("persist reachability report: %w", err)
	}
	return rep, nil
}
