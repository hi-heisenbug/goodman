package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"

	"github.com/hi-heisenbug/goodman/internal/admission"
	"github.com/hi-heisenbug/goodman/internal/api"
	"github.com/hi-heisenbug/goodman/internal/api/ui"
	"github.com/hi-heisenbug/goodman/internal/diff"
	"github.com/hi-heisenbug/goodman/internal/enforce"
	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/notify"
	"github.com/hi-heisenbug/goodman/internal/report"
	"github.com/hi-heisenbug/goodman/internal/store"
)

func runCollector(ctx context.Context, config collectorConfig) error {
	st, err := store.Open(config.DSN)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()
	if err := validateHA(st, config.HAReplicas); err != nil {
		return err
	}

	rules, err := diff.LoadRules(config.RulesPath)
	if err != nil {
		return fmt.Errorf("load rules: %w", err)
	}
	server := newCollectorServer(st, config, rules)
	notifier, err := startNotifier(ctx, server, config)
	if err != nil {
		return err
	}
	startMaintenance(ctx, st, server, notifier, config)
	startAdmissionWebhook(ctx, config)

	scheme := "http"
	if config.TLSCert != "" {
		scheme = "https"
	}
	log.Printf("listening on %s (%s; learning window: %d obs / %s; %d rules)",
		config.Listen, scheme, config.LearnObs, config.LearnAge, len(rules))
	return api.Serve(ctx, config.Listen, server.Router(dashboardFiles()), api.TLSFiles{
		CertFile: config.TLSCert,
		KeyFile:  config.TLSKey,
	})
}

func validateHA(st *store.Store, replicas int) error {
	if replicas > 1 && st.Dialect() != "postgres" {
		return fmt.Errorf("HA collector (%d replicas) requires Postgres; SQLite is single-replica only (set GOODMAN_DSN=postgres://...)", replicas)
	}
	if replicas > 1 {
		log.Printf("HA mode: %d replicas expected; singleton loops use Postgres advisory locks", replicas)
	}
	return nil
}

func newCollectorServer(st *store.Store, config collectorConfig, rules []diff.Rule) *api.Server {
	fingerprints := fingerprint.NewEngine(st, fingerprint.LearningWindow{
		MinObs: config.LearnObs,
		MinAge: config.LearnAge,
	})
	server := api.NewServer(st, fingerprints, diff.NewEngine(st, rules))
	if config.OSVEndpoint != "" {
		server.OSVClient.Endpoint = config.OSVEndpoint
	}
	manager := enforce.NewManager(st, config.EnforceEnabled)
	manager.SetRules(rules)
	server.SetEnforceManager(manager)
	if config.EnforceEnabled {
		log.Printf("enforcement master gate enabled (runtime switch off until goodmanctl enforce on)")
	}
	server.Auth = api.AuthConfig{IngestToken: config.IngestToken, APIToken: config.APIToken}
	server.SetAlertBudget(config.DigestBudget)
	if !server.Auth.Enabled() {
		log.Printf("WARNING: no GOODMAN_INGEST_TOKEN / GOODMAN_API_TOKEN set; the API is unauthenticated (fine locally, not in production)")
	}
	return server
}

func startNotifier(ctx context.Context, server *api.Server, config collectorConfig) (*notify.Notifier, error) {
	if config.WebhookURL == "" {
		return nil, nil
	}
	notifier, err := notify.New(notify.Config{
		URL: config.WebhookURL, Format: config.WebhookFormat, Token: config.WebhookToken,
		MinSeverity: config.WebhookMinSeverity, PublicURL: config.PublicURL,
	})
	if err != nil {
		return nil, fmt.Errorf("webhook: %w", err)
	}
	server.Notifier = notifier
	go notifier.Run(ctx)
	log.Printf("webhook notifications enabled (%s format, min severity %s)",
		config.WebhookFormat, config.WebhookMinSeverity)
	return notifier, nil
}

func startMaintenance(ctx context.Context, st *store.Store, server *api.Server, notifier *notify.Notifier, config collectorConfig) {
	if config.Retention > 0 {
		go pruneLoop(ctx, st, config.Retention)
		log.Printf("retention enabled: resolved alerts pruned after %s (leader-elected when HA)", config.Retention)
	}
	if config.ReachabilityInterval > 0 {
		var osvClient *report.OSVClient
		if config.ReachabilityOSV {
			osvClient = server.OSVClient
		}
		go reachabilityLoop(ctx, st, config.ReachabilityInterval, osvClient)
		log.Printf("reachability refresh enabled: every %s (osv=%t; leader-elected when HA)",
			config.ReachabilityInterval, config.ReachabilityOSV)
	}
	startDigest(ctx, st, notifier, config)
}

func startDigest(ctx context.Context, st *store.Store, notifier *notify.Notifier, config collectorConfig) {
	if config.DigestInterval <= 0 {
		return
	}
	if notifier == nil {
		log.Printf("WARNING: -digest-interval set but -webhook-url is empty; digest delivery disabled")
		return
	}
	go digestLoop(ctx, st, notifier, config.DigestInterval, config.DigestBudget, config.PublicURL)
	log.Printf("weekly digest enabled: every %s (alert budget %d; leader-elected when HA)",
		config.DigestInterval, config.DigestBudget)
}

func startAdmissionWebhook(ctx context.Context, config collectorConfig) {
	if config.AdmissionListen == "" {
		return
	}
	go func() {
		log.Printf("admission webhook listening on %s (injects %s=%s)",
			config.AdmissionListen, admission.NodeOptionsEnv, admission.InjectedNodeOptions)
		if err := admission.Serve(ctx, config.AdmissionListen, config.AdmissionCert, config.AdmissionKey, admission.Handler()); err != nil && ctx.Err() == nil {
			log.Fatalf("admission webhook: %v", err)
		}
	}()
}

func dashboardFiles() fs.FS {
	dist, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		return nil
	}
	return dist
}
