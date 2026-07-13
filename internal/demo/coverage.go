package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hi-heisenbug/goodman/internal/coverage"
)

// SeedCoverage posts a namespace injection snapshot that includes a deliberate
// gap (unlabeled staging) so the Coverage tab is never empty in the demo.
func SeedCoverage(ctx context.Context, c *Client) error {
	body, err := json.Marshal(map[string]any{
		"sensor": "demo",
		"namespaces": []coverage.NamespaceCoverage{
			{Name: "payments", InjectLabel: true, PodsTotal: 4, PodsWithNodeOptions: 4, PodsWithout: 0},
			{Name: "staging", InjectLabel: false, PodsTotal: 3, PodsWithNodeOptions: 0, PodsWithout: 3},
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/coverage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("POST /v1/coverage: %s", resp.Status)
	}
	return nil
}
