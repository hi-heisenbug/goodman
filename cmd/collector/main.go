// The Goodman collector: receives attributed events from sensors, maintains
// fingerprints, runs the diff engine, serves the alerts API and the
// dashboard.
package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/hi-heisenbug/goodman/internal/api"
	"github.com/hi-heisenbug/goodman/internal/api/ui"
	"github.com/hi-heisenbug/goodman/internal/diff"
	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/notify"
	"github.com/hi-heisenbug/goodman/internal/store"
)

func main() {
	var (
		listen    = flag.String("listen", envOr("GOODMAN_LISTEN", ":8844"), "listen address")
		dsn       = flag.String("dsn", envOr("GOODMAN_DSN", "goodman.db"), "postgres://... or sqlite path")
		learnObs  = flag.Int("learn-obs", envIntOr("GOODMAN_LEARN_OBS", 500), "observations required before baseline promotion")
		learnAge  = flag.Duration("learn-min-age", envDurOr("GOODMAN_LEARN_MIN_AGE", 24*time.Hour), "wall-clock age required before baseline promotion")
		rulesPath = flag.String("rules", os.Getenv("GOODMAN_RULES"), "path to high-risk rules JSON (empty = built-in defaults)")

		ingestToken = flag.String("ingest-token", os.Getenv("GOODMAN_INGEST_TOKEN"), "bearer token required on POST /v1/events (empty = open)")
		apiToken    = flag.String("api-token", os.Getenv("GOODMAN_API_TOKEN"), "bearer token required on the alerts/fingerprints/stream API (empty = open)")
		tlsCert     = flag.String("tls-cert", os.Getenv("GOODMAN_TLS_CERT"), "PEM certificate to serve HTTPS (requires -tls-key)")
		tlsKey      = flag.String("tls-key", os.Getenv("GOODMAN_TLS_KEY"), "PEM private key to serve HTTPS (requires -tls-cert)")

		webhookURL    = flag.String("webhook-url", os.Getenv("GOODMAN_WEBHOOK_URL"), "POST alerts to this webhook (empty = disabled)")
		webhookFormat = flag.String("webhook-format", envOr("GOODMAN_WEBHOOK_FORMAT", notify.FormatGeneric), "webhook payload format: generic or slack")
		webhookToken  = flag.String("webhook-token", os.Getenv("GOODMAN_WEBHOOK_TOKEN"), "bearer token sent to the webhook")
		webhookMinSev = flag.String("webhook-min-severity", envOr("GOODMAN_WEBHOOK_MIN_SEVERITY", "WARN"), "lowest severity forwarded to the webhook (INFO|WARN|CRITICAL)")

		retention = flag.Duration("retention", envDurOr("GOODMAN_RETENTION", 0), "prune resolved alerts older than this (0 = keep forever)")
	)
	flag.Parse()
	log.SetPrefix("collector: ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(*dsn)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	rules, err := diff.LoadRules(*rulesPath)
	if err != nil {
		log.Fatalf("load rules: %v", err)
	}

	fpEng := fingerprint.NewEngine(st, fingerprint.LearningWindow{MinObs: *learnObs, MinAge: *learnAge})
	diffEng := diff.NewEngine(st, rules)
	srv := api.NewServer(st, fpEng, diffEng)
	srv.Auth = api.AuthConfig{IngestToken: *ingestToken, APIToken: *apiToken}
	if !srv.Auth.Enabled() {
		log.Printf("WARNING: no GOODMAN_INGEST_TOKEN / GOODMAN_API_TOKEN set; the API is unauthenticated (fine locally, not in production)")
	}

	if *webhookURL != "" {
		notifier, err := notify.New(notify.Config{
			URL:         *webhookURL,
			Format:      *webhookFormat,
			Token:       *webhookToken,
			MinSeverity: *webhookMinSev,
		})
		if err != nil {
			log.Fatalf("webhook: %v", err)
		}
		srv.Notifier = notifier
		go notifier.Run(ctx)
		log.Printf("webhook notifications enabled (%s format, min severity %s)", *webhookFormat, *webhookMinSev)
	}

	if *retention > 0 {
		go pruneLoop(ctx, st, *retention)
		log.Printf("retention enabled: resolved alerts pruned after %s", *retention)
	}

	var uiFS fs.FS
	if sub, err := fs.Sub(ui.Dist, "dist"); err == nil {
		uiFS = sub
	}

	scheme := "http"
	if *tlsCert != "" {
		scheme = "https"
	}
	log.Printf("listening on %s (%s; learning window: %d obs / %s; %d rules)",
		*listen, scheme, *learnObs, *learnAge, len(rules))
	tlsFiles := api.TLSFiles{CertFile: *tlsCert, KeyFile: *tlsKey}
	if err := api.Serve(ctx, *listen, srv.Router(uiFS), tlsFiles); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

// pruneLoop deletes resolved alerts older than the retention window, once at
// startup and then hourly.
func pruneLoop(ctx context.Context, st *store.Store, retention time.Duration) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		pruneCtx, cancel := context.WithTimeout(ctx, time.Minute)
		n, err := st.PruneResolvedAlerts(pruneCtx, time.Now().Add(-retention))
		cancel()
		switch {
		case err != nil && ctx.Err() == nil:
			log.Printf("retention: prune failed: %v", err)
		case n > 0:
			log.Printf("retention: pruned %d resolved alerts older than %s", n, retention)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envIntOr(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDurOr(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
