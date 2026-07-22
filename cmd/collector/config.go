// Environment-backed collector configuration helpers.
package main

import (
	"flag"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/hi-heisenbug/goodman/internal/digest"
	"github.com/hi-heisenbug/goodman/internal/notify"
)

type collectorConfig struct {
	Listen               string
	DSN                  string
	LearnObs             int
	LearnAge             time.Duration
	RulesPath            string
	IngestToken          string
	APIToken             string
	TLSCert              string
	TLSKey               string
	WebhookURL           string
	WebhookFormat        string
	WebhookToken         string
	WebhookMinSeverity   string
	PublicURL            string
	Retention            time.Duration
	ReachabilityInterval time.Duration
	ReachabilityOSV      bool
	OSVEndpoint          string
	DigestInterval       time.Duration
	DigestBudget         int
	HAReplicas           int
	AdmissionListen      string
	AdmissionCert        string
	AdmissionKey         string
	EnforceEnabled       bool
}

func parseCollectorConfig() collectorConfig {
	var config collectorConfig
	flag.StringVar(&config.Listen, "listen", envOr("GOODMAN_LISTEN", ":8844"), "listen address")
	flag.StringVar(&config.DSN, "dsn", envOr("GOODMAN_DSN", "goodman.db"), "postgres://... or sqlite path")
	flag.IntVar(&config.LearnObs, "learn-obs", envIntOr("GOODMAN_LEARN_OBS", 500), "observations required before baseline promotion")
	flag.DurationVar(&config.LearnAge, "learn-min-age", envDurOr("GOODMAN_LEARN_MIN_AGE", 24*time.Hour), "wall-clock age required before baseline promotion")
	flag.StringVar(&config.RulesPath, "rules", os.Getenv("GOODMAN_RULES"), "path to high-risk rules JSON (empty = built-in defaults)")
	flag.StringVar(&config.IngestToken, "ingest-token", os.Getenv("GOODMAN_INGEST_TOKEN"), "bearer token required on POST /v1/events (empty = open)")
	flag.StringVar(&config.APIToken, "api-token", os.Getenv("GOODMAN_API_TOKEN"), "bearer token required on the alerts/fingerprints/stream API (empty = open)")
	flag.StringVar(&config.TLSCert, "tls-cert", os.Getenv("GOODMAN_TLS_CERT"), "PEM certificate to serve HTTPS (requires -tls-key)")
	flag.StringVar(&config.TLSKey, "tls-key", os.Getenv("GOODMAN_TLS_KEY"), "PEM private key to serve HTTPS (requires -tls-cert)")
	flag.StringVar(&config.WebhookURL, "webhook-url", os.Getenv("GOODMAN_WEBHOOK_URL"), "POST alerts to this webhook (empty = disabled)")
	flag.StringVar(&config.WebhookFormat, "webhook-format", envOr("GOODMAN_WEBHOOK_FORMAT", notify.FormatGeneric), "webhook payload format: generic or slack")
	flag.StringVar(&config.WebhookToken, "webhook-token", os.Getenv("GOODMAN_WEBHOOK_TOKEN"), "bearer token sent to the webhook")
	flag.StringVar(&config.WebhookMinSeverity, "webhook-min-severity", envOr("GOODMAN_WEBHOOK_MIN_SEVERITY", "WARN"), "lowest severity forwarded to the webhook (INFO|WARN|CRITICAL)")
	flag.StringVar(&config.PublicURL, "public-url", os.Getenv("GOODMAN_PUBLIC_URL"), "dashboard base URL for Slack deep links and digests")
	flag.DurationVar(&config.Retention, "retention", envDurOr("GOODMAN_RETENTION", 0), "prune resolved alerts older than this (0 = keep forever)")
	flag.DurationVar(&config.ReachabilityInterval, "reachability-interval", envDurOr("GOODMAN_REACHABILITY_INTERVAL", 0), "recompute stored reachability reports on this cadence (0 = disabled)")
	flag.BoolVar(&config.ReachabilityOSV, "reachability-osv", envBoolOr("GOODMAN_REACHABILITY_OSV", false), "enrich scheduled reachability recomputes with OSV.dev (needs egress)")
	flag.StringVar(&config.OSVEndpoint, "osv-endpoint", os.Getenv("GOODMAN_OSV_ENDPOINT"), "OSV querybatch endpoint (empty = public OSV.dev)")
	flag.DurationVar(&config.DigestInterval, "digest-interval", envDurOr("GOODMAN_DIGEST_INTERVAL", 0), "emit a weekly digest to the webhook on this cadence (0 = disabled; requires -webhook-url)")
	flag.IntVar(&config.DigestBudget, "digest-alert-budget", envIntOr("GOODMAN_DIGEST_ALERT_BUDGET", digest.DefaultAlertBudget), "soft open-alert budget quoted in the digest")
	flag.IntVar(&config.HAReplicas, "ha-replicas", envIntOr("GOODMAN_HA_REPLICAS", 1), "expected collector replica count for HA (Postgres required when >1)")
	flag.StringVar(&config.AdmissionListen, "admission-listen", os.Getenv("GOODMAN_ADMISSION_LISTEN"), "serve the NODE_OPTIONS mutating webhook on this address (empty = disabled)")
	flag.StringVar(&config.AdmissionCert, "admission-tls-cert", os.Getenv("GOODMAN_ADMISSION_TLS_CERT"), "PEM cert for the admission webhook (required when -admission-listen is set)")
	flag.StringVar(&config.AdmissionKey, "admission-tls-key", os.Getenv("GOODMAN_ADMISSION_TLS_KEY"), "PEM key for the admission webhook")
	flag.BoolVar(&config.EnforceEnabled, "enforce-enabled", envBoolOr("GOODMAN_ENFORCE_ENABLED", false), "master gate for kernel LSM enforcement (default false)")
	flag.Parse()
	return config
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
		log.Printf("configuration: %s=%q is not an integer; using %d", k, v, def)
	}
	return def
}

func envDurOr(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Printf("configuration: %s=%q is not a Go duration; using %s", k, v, def)
	}
	return def
}

func envBoolOr(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		log.Printf("configuration: %s=%q is not a boolean; using %t", k, v, def)
		return def
	}
	return parsed
}
