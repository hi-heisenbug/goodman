// Package notify delivers alerts to an external webhook (generic JSON or
// Slack-compatible). Delivery is asynchronous with bounded retries so the
// ingest path never blocks on a slow or unreachable endpoint.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/hi-heisenbug/goodman/internal/model"
)

var deliveries = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "goodman_collector_notifications_total",
	Help: "Webhook alert notifications by result (delivered|failed|dropped|filtered).",
}, []string{"result"})

const (
	FormatGeneric = "generic"
	FormatSlack   = "slack"
)

// Config configures a webhook Notifier. URL is required; everything else has
// a sensible default.
type Config struct {
	URL         string
	Format      string        // FormatGeneric (default) or FormatSlack
	Token       string        // optional bearer token sent to the webhook
	MinSeverity string        // lowest severity forwarded (default WARN)
	Timeout     time.Duration // per-attempt HTTP timeout (default 10s)
	MaxRetries  int           // re-delivery attempts after the first (default 2)
}

// Notifier accepts alerts on a bounded queue and posts them to the webhook
// from a single background worker. When the queue is full, alerts are dropped
// (and counted) rather than blocking the caller.
type Notifier struct {
	cfg     Config
	client  *http.Client
	queue   chan model.Alert
	minRank int
	backoff func(attempt int) time.Duration
}

var severityRank = map[string]int{
	model.SeverityInfo:     0,
	model.SeverityWarn:     1,
	model.SeverityCritical: 2,
}

// New validates cfg and returns a Notifier. Call Run to start delivery.
func New(cfg Config) (*Notifier, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("notify: webhook URL is required")
	}
	if cfg.Format == "" {
		cfg.Format = FormatGeneric
	}
	if cfg.Format != FormatGeneric && cfg.Format != FormatSlack {
		return nil, fmt.Errorf("notify: unknown format %q (want %s or %s)", cfg.Format, FormatGeneric, FormatSlack)
	}
	if cfg.MinSeverity == "" {
		cfg.MinSeverity = model.SeverityWarn
	}
	rank, ok := severityRank[cfg.MinSeverity]
	if !ok {
		return nil, fmt.Errorf("notify: unknown min severity %q", cfg.MinSeverity)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	} else if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 2
	}
	return &Notifier{
		cfg:     cfg,
		client:  &http.Client{Timeout: cfg.Timeout},
		queue:   make(chan model.Alert, 256),
		minRank: rank,
		backoff: func(attempt int) time.Duration { return time.Duration(attempt+1) * 2 * time.Second },
	}, nil
}

// Notify enqueues an alert for delivery. It never blocks: below-threshold
// alerts are filtered and a full queue drops (both counted in metrics).
func (n *Notifier) Notify(a model.Alert) {
	if severityRank[a.Severity] < n.minRank {
		deliveries.WithLabelValues("filtered").Inc()
		return
	}
	select {
	case n.queue <- a:
	default:
		deliveries.WithLabelValues("dropped").Inc()
		log.Printf("notify: queue full, dropped alert %s", a.ID)
	}
}

// Run delivers queued alerts until ctx is cancelled. It drains anything
// already queued before returning.
func (n *Notifier) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case a := <-n.queue:
					n.deliver(context.Background(), a)
				default:
					return
				}
			}
		case a := <-n.queue:
			n.deliver(ctx, a)
		}
	}
}

func (n *Notifier) deliver(ctx context.Context, a model.Alert) {
	payload, err := n.payload(a)
	if err != nil {
		deliveries.WithLabelValues("failed").Inc()
		log.Printf("notify: encode alert %s: %v", a.ID, err)
		return
	}
	for attempt := 0; ; attempt++ {
		err := n.post(ctx, payload)
		if err == nil {
			deliveries.WithLabelValues("delivered").Inc()
			return
		}
		if attempt >= n.cfg.MaxRetries || ctx.Err() != nil {
			deliveries.WithLabelValues("failed").Inc()
			log.Printf("notify: alert %s failed after %d attempts: %v", a.ID, attempt+1, err)
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(n.backoff(attempt)):
		}
	}
}

func (n *Notifier) post(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if n.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+n.cfg.Token)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}

func (n *Notifier) payload(a model.Alert) ([]byte, error) {
	if n.cfg.Format == FormatSlack {
		return json.Marshal(slackMessage(a))
	}
	return json.Marshal(map[string]any{"type": "goodman.alert", "alert": a})
}

func slackMessage(a model.Alert) map[string]any {
	old := a.OldVersion
	if old == "" {
		old = "(none)"
	}
	text := fmt.Sprintf("*Goodman %s alert*: `%s` dependency `%s` drifted %s → %s",
		a.Severity, a.Service, a.Package, old, a.NewVersion)
	for _, b := range a.NewBehaviors {
		text += fmt.Sprintf("\n• `%s`", b)
	}
	text += fmt.Sprintf("\nDetected %s · alert id `%s`",
		model.NsToTime(a.DetectedAt).UTC().Format(time.RFC3339), a.ID)
	return map[string]any{"text": text}
}
