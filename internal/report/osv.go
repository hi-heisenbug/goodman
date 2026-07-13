package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Vulnerability is a known advisory for a package version, from OSV.dev.
type Vulnerability struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Severity string `json:"severity"` // best-effort label: CRITICAL/HIGH/MODERATE/LOW/UNKNOWN
}

// OSVClient queries the OSV.dev batch API. The zero value is usable; it uses
// the public endpoint and a 20s timeout.
type OSVClient struct {
	Endpoint string
	client   *http.Client
}

const defaultOSVEndpoint = "https://api.osv.dev/v1/querybatch"

func NewOSVClient() *OSVClient {
	return &OSVClient{Endpoint: defaultOSVEndpoint, client: &http.Client{Timeout: 20 * time.Second}}
}

// Query returns vulnerabilities per package (keyed by "name@version") for the
// given packages. The batch endpoint returns only IDs, so this resolves each
// hit to a summary/severity via the per-vuln endpoint (bounded by the number
// of distinct advisories, typically small).
func (c *OSVClient) Query(ctx context.Context, pkgs []DeclaredPackage) (map[string][]Vulnerability, error) {
	if c.client == nil {
		c.client = &http.Client{Timeout: 20 * time.Second}
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultOSVEndpoint
	}

	type osvQuery struct {
		Version string            `json:"version"`
		Package map[string]string `json:"package"`
	}
	body := struct {
		Queries []osvQuery `json:"queries"`
	}{}
	for _, p := range pkgs {
		body.Queries = append(body.Queries, osvQuery{
			Version: p.Version,
			Package: map[string]string{"name": p.Name, "ecosystem": "npm"},
		})
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("osv querybatch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("osv querybatch returned %s", resp.Status)
	}
	var batch struct {
		Results []struct {
			Vulns []struct {
				ID string `json:"id"`
			} `json:"vulns"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return nil, err
	}

	out := map[string][]Vulnerability{}
	cache := map[string]Vulnerability{}
	for i, res := range batch.Results {
		if i >= len(pkgs) || len(res.Vulns) == 0 {
			continue
		}
		key := pkgs[i].Name + "@" + pkgs[i].Version
		for _, v := range res.Vulns {
			vuln, ok := cache[v.ID]
			if !ok {
				vuln = c.resolve(ctx, endpoint, v.ID)
				cache[v.ID] = vuln
			}
			out[key] = append(out[key], vuln)
		}
	}
	return out, nil
}

// resolve fetches advisory detail; on any error it degrades to just the ID so
// the report still lists the vulnerability.
func (c *OSVClient) resolve(ctx context.Context, batchEndpoint, id string) Vulnerability {
	base := "https://api.osv.dev/v1/vulns/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+id, nil)
	if err != nil {
		return Vulnerability{ID: id, Severity: "UNKNOWN"}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return Vulnerability{ID: id, Severity: "UNKNOWN"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Vulnerability{ID: id, Severity: "UNKNOWN"}
	}
	var v struct {
		Summary          string `json:"summary"`
		DatabaseSpecific struct {
			Severity string `json:"severity"`
		} `json:"database_specific"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return Vulnerability{ID: id, Severity: "UNKNOWN"}
	}
	sev := v.DatabaseSpecific.Severity
	if sev == "" {
		sev = "UNKNOWN"
	}
	return Vulnerability{ID: id, Summary: v.Summary, Severity: sev}
}
