package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

// maxBatch is the OSV querybatch per-request query cap. Larger package sets
// are split into multiple batches.
const maxBatch = 1000

// resolveConcurrency bounds how many advisory-detail GETs run at once.
const resolveConcurrency = 8

// Query returns vulnerabilities per package (keyed by "name@version") for the
// given packages. Packages are queried in batches of at most maxBatch; the
// batch endpoint returns only IDs, so each distinct advisory is then resolved
// to a summary/severity concurrently (bounded by resolveConcurrency).
func (c *OSVClient) Query(ctx context.Context, pkgs []DeclaredPackage) (map[string][]Vulnerability, error) {
	if c.client == nil {
		c.client = &http.Client{Timeout: 20 * time.Second}
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultOSVEndpoint
	}

	// out maps "name@version" -> advisory IDs; detail is resolved afterward so
	// each distinct advisory is fetched at most once, concurrently.
	out := map[string][]Vulnerability{}
	ids := map[string]bool{}
	for start := 0; start < len(pkgs); start += maxBatch {
		end := start + maxBatch
		if end > len(pkgs) {
			end = len(pkgs)
		}
		if err := c.queryBatch(ctx, endpoint, pkgs[start:end], out, ids); err != nil {
			return nil, err
		}
	}

	detail := c.resolveAll(ctx, endpoint, ids)
	for key, vulns := range out {
		for i := range vulns {
			if d, ok := detail[vulns[i].ID]; ok {
				out[key][i] = d
			}
		}
	}
	return out, nil
}

// queryBatch posts one <=1000-query batch and records advisory IDs per package
// into out (and the global id set) without resolving detail.
func (c *OSVClient) queryBatch(ctx context.Context, endpoint string, pkgs []DeclaredPackage, out map[string][]Vulnerability, ids map[string]bool) error {
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
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("osv querybatch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("osv querybatch returned %s", resp.Status)
	}
	var batch struct {
		Results []struct {
			Vulns []struct {
				ID string `json:"id"`
			} `json:"vulns"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return err
	}
	for i, res := range batch.Results {
		if i >= len(pkgs) || len(res.Vulns) == 0 {
			continue
		}
		key := pkgs[i].Name + "@" + pkgs[i].Version
		for _, v := range res.Vulns {
			out[key] = append(out[key], Vulnerability{ID: v.ID})
			ids[v.ID] = true
		}
	}
	return nil
}

// resolveAll fetches advisory detail for every id concurrently (bounded).
func (c *OSVClient) resolveAll(ctx context.Context, endpoint string, ids map[string]bool) map[string]Vulnerability {
	detail := make(map[string]Vulnerability, len(ids))
	var mu sync.Mutex
	sem := make(chan struct{}, resolveConcurrency)
	var wg sync.WaitGroup
	for id := range ids {
		id := id
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			v := c.resolve(ctx, endpoint, id)
			mu.Lock()
			detail[id] = v
			mu.Unlock()
		}()
	}
	wg.Wait()
	return detail
}

// resolve fetches advisory detail; on any error it degrades to just the ID so
// the report still lists the vulnerability.
func (c *OSVClient) resolve(ctx context.Context, batchEndpoint, id string) Vulnerability {
	detailURL, err := osvDetailURL(batchEndpoint, id)
	if err != nil {
		return Vulnerability{ID: id, Severity: "UNKNOWN"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
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

func osvDetailURL(batchEndpoint, id string) (string, error) {
	u, err := url.Parse(batchEndpoint)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimSuffix(u.Path, "/")
	basePath = strings.TrimSuffix(basePath, "/querybatch")
	u.Path = strings.TrimSuffix(basePath, "/") + "/vulns/" + url.PathEscape(id)
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
