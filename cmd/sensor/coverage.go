// Kubernetes attribution coverage reporting.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/hi-heisenbug/goodman/internal/coverage"
)

// reportCoverageLoop posts namespace injection coverage to the collector when
// running in-cluster. Outside a cluster it is a no-op (ScanClusterCoverage fails).
func reportCoverageLoop(ctx context.Context, client *http.Client, baseURL, token, sensor string) {
	interval := envDurOr("GOODMAN_COVERAGE_INTERVAL", 5*time.Minute)
	t := time.NewTicker(interval)
	defer t.Stop()
	report := func() {
		rows, err := coverage.ScanClusterCoverage(sensor)
		if err != nil {
			return // not in cluster, or transient API error
		}
		body, err := json.Marshal(map[string]any{"sensor": sensor, "namespaces": rows})
		if err != nil {
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/coverage", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("coverage report: %v", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Printf("coverage report: collector returned %s", resp.Status)
		}
	}
	report()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			report()
		}
	}
}
