// The Goodman collector: receives attributed events from sensors, maintains
// fingerprints, runs the diff engine, serves the alerts API and the
// dashboard.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/hi-heisenbug/goodman/internal/admission"
	"github.com/hi-heisenbug/goodman/internal/api"
	"github.com/hi-heisenbug/goodman/internal/api/ui"
	"github.com/hi-heisenbug/goodman/internal/diff"
	"github.com/hi-heisenbug/goodman/internal/digest"
	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/notify"
	"github.com/hi-heisenbug/goodman/internal/report"
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
		publicURL     = flag.String("public-url", os.Getenv("GOODMAN_PUBLIC_URL"), "dashboard base URL for Slack deep links and digests")

		retention = flag.Duration("retention", envDurOr("GOODMAN_RETENTION", 0), "prune resolved alerts older than this (0 = keep forever)")

		reachInterval = flag.Duration("reachability-interval", envDurOr("GOODMAN_REACHABILITY_INTERVAL", 0), "recompute stored reachability reports on this cadence (0 = disabled)")
		reachOSV      = flag.Bool("reachability-osv", os.Getenv("GOODMAN_REACHABILITY_OSV") == "1" || os.Getenv("GOODMAN_REACHABILITY_OSV") == "true", "enrich scheduled reachability recomputes with OSV.dev (needs egress)")

		digestInterval = flag.Duration("digest-interval", envDurOr("GOODMAN_DIGEST_INTERVAL", 0), "emit a weekly digest to the webhook on this cadence (0 = disabled; requires -webhook-url)")
		digestBudget   = flag.Int("digest-alert-budget", envIntOr("GOODMAN_DIGEST_ALERT_BUDGET", digest.DefaultAlertBudget), "soft open-alert budget quoted in the digest")

		admissionListen = flag.String("admission-listen", os.Getenv("GOODMAN_ADMISSION_LISTEN"), "serve the NODE_OPTIONS mutating webhook on this address (empty = disabled)")
		admissionCert   = flag.String("admission-tls-cert", os.Getenv("GOODMAN_ADMISSION_TLS_CERT"), "PEM cert for the admission webhook (required when -admission-listen is set)")
		admissionKey    = flag.String("admission-tls-key", os.Getenv("GOODMAN_ADMISSION_TLS_KEY"), "PEM key for the admission webhook")
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
	srv.SetAlertBudget(*digestBudget)
	if !srv.Auth.Enabled() {
		log.Printf("WARNING: no GOODMAN_INGEST_TOKEN / GOODMAN_API_TOKEN set; the API is unauthenticated (fine locally, not in production)")
	}

	var notifier *notify.Notifier
	if *webhookURL != "" {
		var err error
		notifier, err = notify.New(notify.Config{
			URL:         *webhookURL,
			Format:      *webhookFormat,
			Token:       *webhookToken,
			MinSeverity: *webhookMinSev,
			PublicURL:   *publicURL,
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

	if *reachInterval > 0 {
		go reachabilityLoop(ctx, st, *reachInterval, *reachOSV)
		log.Printf("reachability refresh enabled: every %s (osv=%t)", *reachInterval, *reachOSV)
	}

	if *digestInterval > 0 {
		if notifier == nil {
			log.Printf("WARNING: -digest-interval set but -webhook-url is empty; digest delivery disabled")
		} else {
			go digestLoop(ctx, st, notifier, *digestInterval, *digestBudget, *publicURL)
			log.Printf("weekly digest enabled: every %s (alert budget %d)", *digestInterval, *digestBudget)
		}
	}

	if *admissionListen != "" {
		go func() {
			log.Printf("admission webhook listening on %s (injects %s=%s)",
				*admissionListen, admission.NodeOptionsEnv, admission.InjectedNodeOptions)
			if err := admission.Serve(ctx, *admissionListen, *admissionCert, *admissionKey, admission.Handler()); err != nil && ctx.Err() == nil {
				log.Fatalf("admission webhook: %v", err)
			}
		}()
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

// reachabilityLoop recomputes stored reachability snapshots against the latest
// fingerprints, once at startup and then every interval, so the dashboard
// shows current numbers without a manual re-upload.
func reachabilityLoop(ctx context.Context, st *store.Store, interval time.Duration, osv bool) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		func() {
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			lockfiles, err := st.ListLockfiles(runCtx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("reachability: list lockfiles: %v", err)
				}
				return
			}
			if len(lockfiles) == 0 {
				return
			}
			refresh := make([]report.Lockfile, len(lockfiles))
			for i, lf := range lockfiles {
				refresh[i] = report.Lockfile{Service: lf.Service, Content: lf.Content}
			}
			var osvClient *report.OSVClient
			if osv {
				osvClient = report.NewOSVClient()
			}
			n, err := report.RefreshAll(runCtx, st, refresh, osvClient, uint64(time.Now().UnixNano()))
			if err != nil && ctx.Err() == nil {
				log.Printf("reachability: refresh: %v", err)
			} else if n > 0 {
				log.Printf("reachability: refreshed %d service report(s)", n)
			}
		}()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// digestLoop posts a weekly heartbeat to the configured webhook once at
// startup and then every interval, so a quiet POV still speaks on day one.
func digestLoop(ctx context.Context, st *store.Store, n *notify.Notifier, interval time.Duration, budget int, publicURL string) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		func() {
			runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			d, err := digest.Build(runCtx, st, budget, publicURL)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("digest: build: %v", err)
				}
				return
			}
			var payload []byte
			if n.Format() == notify.FormatSlack {
				payload, err = json.Marshal(d.SlackPayload())
			} else {
				payload, err = json.Marshal(d.GenericPayload())
			}
			if err != nil {
				log.Printf("digest: encode: %v", err)
				return
			}
			if err := n.PostJSON(runCtx, payload); err != nil && ctx.Err() == nil {
				log.Printf("digest: deliver: %v", err)
				return
			}
			log.Printf("digest: delivered (open alerts=%d, executed=%d)", d.OpenAlerts, d.ExecutedCount)
		}()
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
