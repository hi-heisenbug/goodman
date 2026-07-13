// Package demo seeds a local collector for the five-minute product wow:
// realistic fingerprints and alerts, a preloaded reachability report
// (1,400 declared / 240 executed), and a live event-stream attack replay.
package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/report"
)

// Headline numbers the Reachability tab must show on first load.
const (
	DeclaredCount = 1400
	ExecutedCount = 240
)

// Client talks to a running collector over HTTP (no auth; local demo).
type Client struct {
	BaseURL string
	Sensor  string
	HTTP    *http.Client
}

// NewClient returns a demo client pointed at baseURL.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Sensor:  "demo",
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// WaitHealthy polls /v1/healthz until it succeeds or ctx ends.
func (c *Client) WaitHealthy(ctx context.Context) error {
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/healthz", nil)
		if err != nil {
			return err
		}
		resp, err := c.HTTP.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("collector not healthy at %s: %w", c.BaseURL, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// PostEvents POSTs an EventBatch to /v1/events.
func (c *Client) PostEvents(ctx context.Context, events []model.Attributed) error {
	if len(events) == 0 {
		return nil
	}
	body, err := json.Marshal(model.EventBatch{Sensor: c.Sensor, Events: events})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/events", bytes.NewReader(body))
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
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("POST /v1/events: %s: %s", resp.Status, b)
	}
	return nil
}

// PostReport uploads a lockfile to /v1/report. When persist is set the
// collector stores it so the dashboard Reachability tab loads on first paint.
func (c *Client) PostReport(ctx context.Context, lockfile []byte, service string, persist bool) (*report.Report, error) {
	u, err := url.Parse(c.BaseURL + "/v1/report")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if service != "" {
		q.Set("service", service)
	}
	if persist {
		q.Set("persist", "1")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(lockfile))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("POST /v1/report: %s: %s", resp.Status, b)
	}
	var rep report.Report
	if err := json.NewDecoder(resp.Body).Decode(&rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

// GetAlerts returns open alerts from the collector.
func (c *Client) GetAlerts(ctx context.Context) ([]model.Alert, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/alerts", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("GET /v1/alerts: %s: %s", resp.Status, b)
	}
	var alerts []model.Alert
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return nil, err
	}
	return alerts, nil
}

// GetReport returns the persisted reachability report (no service filter).
func (c *Client) GetReport(ctx context.Context) (*report.Report, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/report", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no stored report")
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("GET /v1/report: %s: %s", resp.Status, b)
	}
	var wrap struct {
		Report report.Report `json:"report"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrap); err != nil {
		return nil, err
	}
	return &wrap.Report, nil
}

// EventTypeFromBehavior maps a canonical behavior string to its event type.
func EventTypeFromBehavior(behavior string) model.EventType {
	switch {
	case strings.HasPrefix(behavior, "CONNECT "):
		return model.EventNetConnect
	case strings.HasPrefix(behavior, "EXEC "):
		return model.EventProcExec
	default:
		return model.EventFileOpen
	}
}

// attr builds one attributed event.
func attr(service, pkg, version, behavior string, ts uint64) model.Attributed {
	return model.Attributed{
		Service:   service,
		Package:   pkg,
		Version:   version,
		Type:      EventTypeFromBehavior(behavior),
		Behavior:  behavior,
		Timestamp: ts,
	}
}

// GenerateLockfile builds a synthetic npm lockfile v3 with n packages
// named demo-dep-0001 … demo-dep-NNNN at version 1.0.0.
func GenerateLockfile(n int) []byte {
	packages := make(map[string]any, n+1)
	packages[""] = map[string]any{"name": "demo-app", "version": "1.0.0"}
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("demo-dep-%04d", i)
		packages["node_modules/"+name] = map[string]any{"version": "1.0.0"}
	}
	raw, _ := json.Marshal(map[string]any{
		"name":            "demo-app",
		"version":         "1.0.0",
		"lockfileVersion": 3,
		"packages":        packages,
	})
	return raw
}

// PackageName returns the synthetic package name for index i (1-based).
func PackageName(i int) string {
	return fmt.Sprintf("demo-dep-%04d", i)
}
