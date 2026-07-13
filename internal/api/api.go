// Package api serves the collector's HTTP surface: sensor ingestion, the
// alerts/fingerprints API for the dashboard and goodmanctl, an SSE event
// stream, Prometheus metrics, and the embedded dashboard UI.
package api

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hi-heisenbug/goodman/internal/coverage"
	"github.com/hi-heisenbug/goodman/internal/diff"
	"github.com/hi-heisenbug/goodman/internal/enforce"
	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/report"
	"github.com/hi-heisenbug/goodman/internal/store"
)

var (
	eventsIngested = promauto.NewCounter(prometheus.CounterOpts{
		Name: "goodman_collector_events_ingested_total",
		Help: "Attributed events received from sensors."})
	alertsEmitted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "goodman_collector_alerts_total",
		Help: "Alerts emitted by the diff engine."}, []string{"severity"})
)

// Alerter receives every alert the diff engine emits. Implemented by
// notify.Notifier; kept as an interface so api does not depend on delivery.
type Alerter interface {
	Notify(model.Alert)
}

type Server struct {
	store   *store.Store
	fpEng   *fingerprint.Engine
	diffEng *diff.Engine
	cover   *coverage.Registry
	enforce *enforce.Manager

	// Auth protects the HTTP surface; zero value leaves it open (local dev).
	Auth AuthConfig
	// Notifier, when set, receives every emitted alert (webhook delivery).
	Notifier Alerter

	mu   sync.Mutex
	subs map[chan []byte]bool // SSE subscribers
}

func NewServer(s *store.Store, fpEng *fingerprint.Engine, diffEng *diff.Engine) *Server {
	return &Server{
		store: s, fpEng: fpEng, diffEng: diffEng,
		cover: coverage.NewRegistry(),
		subs:  map[chan []byte]bool{},
	}
}

// SetEnforceManager wires the enforcement state manager (optional).
func (s *Server) SetEnforceManager(m *enforce.Manager) {
	s.enforce = m
}

// SetAlertBudget configures the Coverage panel's soft daily alert target.
func (s *Server) SetAlertBudget(n int) {
	if s.cover != nil {
		s.cover.SetAlertBudget(n)
	}
}

func (s *Server) Router(ui fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/v1/readyz", s.handleReadyz)
	r.Post("/v1/events", requireToken(s.Auth.IngestToken, false, s.handleIngest))
	r.Post("/v1/coverage", requireToken(s.Auth.IngestToken, false, s.handlePostCoverage))
	r.Get("/v1/coverage", requireToken(s.Auth.APIToken, false, s.handleGetCoverage))
	r.Get("/v1/alerts", requireToken(s.Auth.APIToken, false, s.handleListAlerts))
	r.Post("/v1/alerts/{id}/ack", requireToken(s.Auth.APIToken, false, s.alertStatusHandler(model.AlertAcknowledged)))
	r.Post("/v1/alerts/{id}/resolve", requireToken(s.Auth.APIToken, false, s.alertStatusHandler(model.AlertResolved)))
	r.Get("/v1/fingerprints", requireToken(s.Auth.APIToken, false, s.handleListFingerprints))
	r.Get("/v1/fingerprints/export", requireToken(s.Auth.APIToken, false, s.handleExportFingerprints))
	r.Post("/v1/fingerprints/import", requireToken(s.Auth.APIToken, false, s.handleImportFingerprints))
	r.Post("/v1/report", requireToken(s.Auth.APIToken, false, s.handleReport))
	r.Get("/v1/report", requireToken(s.Auth.APIToken, false, s.handleGetReport))
	// EventSource cannot set headers, so the stream also accepts ?token=.
	r.Get("/v1/stream", requireToken(s.Auth.APIToken, true, s.handleStream))
	r.Get("/v1/enforce/state", requireToken(s.Auth.IngestToken, false, s.handleEnforceState))
	r.Get("/v1/enforce", requireToken(s.Auth.APIToken, false, s.handleEnforceStatus))
	r.Post("/v1/enforce/on", requireToken(s.Auth.APIToken, false, s.handleEnforceOn))
	r.Post("/v1/enforce/off", requireToken(s.Auth.APIToken, false, s.handleEnforceOff))
	r.Handle("/metrics", promhttp.Handler())

	if ui != nil {
		fileServer := http.FileServer(http.FS(ui))
		r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
			p := strings.TrimPrefix(req.URL.Path, "/")
			if p != "" {
				if f, err := ui.Open(p); err == nil {
					f.Close()
					fileServer.ServeHTTP(w, req)
					return
				}
			}
			// SPA fallback
			req.URL.Path = "/"
			fileServer.ServeHTTP(w, req)
		})
	}
	return r
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// handleReadyz reports whether the collector can serve traffic: unlike
// healthz (process liveness) it fails when the database is unreachable, so
// Kubernetes stops routing to a collector that cannot persist events.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleIngest accepts a (possibly gzipped) EventBatch from a sensor, runs
// fingerprint aggregation and the diff engine, and broadcasts to streams.
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	var body io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "bad gzip", http.StatusBadRequest)
			return
		}
		defer gz.Close()
		body = gz
	}
	var batch model.EventBatch
	if err := json.NewDecoder(io.LimitReader(body, 64<<20)).Decode(&batch); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	eventsIngested.Add(float64(len(batch.Events)))
	for i := range batch.Events {
		if batch.Events[i].Sensor == "" {
			batch.Events[i].Sensor = batch.Sensor
		}
	}
	if s.cover != nil {
		s.cover.ObserveIngest(batch.Sensor, batch.Events, time.Now())
	}

	ctx := r.Context()
	if len(batch.Events) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ingested": 0, "alerts": 0})
		return
	}

	var learn []model.Attributed
	var denied []model.Attributed
	for _, ev := range batch.Events {
		if ev.Denied {
			denied = append(denied, ev)
		} else {
			learn = append(learn, ev)
		}
	}

	updates, err := s.fpEng.Ingest(ctx, learn)
	if err != nil {
		log.Printf("api: ingest: %v", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	var alerts []model.Alert
	for _, up := range updates {
		for _, b := range up.FreshBehaviors {
			s.recordBlockBehavior(b)
		}
		a, err := s.diffEng.React(ctx, up)
		if err != nil {
			log.Printf("api: diff: %v", err)
			continue
		}
		if a != nil {
			alerts = append(alerts, *a)
			for _, b := range a.NewBehaviors {
				s.recordBlockBehavior(b)
			}
			alertsEmitted.WithLabelValues(a.Severity).Inc()
			if s.Notifier != nil {
				s.Notifier.Notify(*a)
			}
		}
	}
	for _, ev := range denied {
		s.recordBlockBehavior(ev.Behavior)
		a, err := s.diffEng.ReactDenied(ctx, ev)
		if err != nil {
			log.Printf("api: denied: %v", err)
			continue
		}
		if a != nil {
			alerts = append(alerts, *a)
			alertsEmitted.WithLabelValues(a.Severity).Inc()
			if s.Notifier != nil {
				s.Notifier.Notify(*a)
			}
		}
	}
	s.broadcast("events", batch.Events)
	if len(alerts) > 0 {
		s.broadcast("alerts", alerts)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ingested": len(batch.Events), "alerts": len(alerts)})
}

func (s *Server) recordBlockBehavior(behavior string) {
	if s.enforce != nil && behavior != "" {
		s.enforce.RecordBehavior(behavior)
	}
}

func (s *Server) handleEnforceState(w http.ResponseWriter, r *http.Request) {
	sensor := r.URL.Query().Get("sensor")
	active := r.URL.Query().Get("enforcement_active") == "true"
	if s.enforce == nil {
		if sensor != "" {
			// no-op when enforcement not configured
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "rev": 0, "verdicts": enforce.VerdictSet{}})
		return
	}
	if sensor != "" {
		s.enforce.RecordSensorHeartbeat(sensor, active)
	}
	enabled, rev, vs := s.enforce.StateForSensor()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":  enabled,
		"rev":      rev,
		"verdicts": vs,
		"skipped":  vs.Skipped,
	})
}

func (s *Server) handleEnforceStatus(w http.ResponseWriter, r *http.Request) {
	if s.enforce == nil {
		writeJSON(w, http.StatusOK, map[string]any{"master_gate": false, "enabled": false})
		return
	}
	enabled, master, rev, vs, sensors := s.enforce.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"master_gate": master,
		"enabled":     enabled,
		"rev":         rev,
		"verdicts":    vs,
		"skipped":     vs.Skipped,
		"sensors":     sensors,
	})
}

func (s *Server) handleEnforceOn(w http.ResponseWriter, r *http.Request) {
	if s.enforce == nil {
		http.Error(w, "enforcement not configured", http.StatusNotFound)
		return
	}
	if err := s.enforce.SetEnabled(r.Context(), true); err != nil {
		if errors.Is(err, enforce.ErrMasterGateOff) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true})
}

func (s *Server) handleEnforceOff(w http.ResponseWriter, r *http.Request) {
	if s.enforce == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	if err := s.enforce.SetEnabled(r.Context(), false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.store.ListAlerts(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.enrichAlerts(r.Context(), alerts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if alerts == nil {
		alerts = []model.Alert{}
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (s *Server) enrichAlerts(ctx context.Context, alerts []model.Alert) error {
	for i := range alerts {
		if alerts[i].OldVersion == "" || len(alerts[i].BaselineBehaviors) > 0 {
			continue
		}
		fp, err := s.store.GetFingerprint(ctx, alerts[i].Service, alerts[i].Package, alerts[i].OldVersion)
		if err != nil {
			return err
		}
		if fp == nil {
			continue
		}
		alerts[i].BaselineBehaviors = behaviorKeys(fp.Behaviors)
	}
	return nil
}

func behaviorKeys(behaviors map[string]model.BehaviorStat) []string {
	keys := make([]string, 0, len(behaviors))
	for b := range behaviors {
		keys = append(keys, b)
	}
	sort.Strings(keys)
	return keys
}

func (s *Server) alertStatusHandler(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := s.store.SetAlertStatus(r.Context(), chi.URLParam(r, "id"), status)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "no such alert", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": status})
	}
}

func (s *Server) handleListFingerprints(w http.ResponseWriter, r *http.Request) {
	fps, err := s.store.ListFingerprints(r.Context(),
		r.URL.Query().Get("service"), r.URL.Query().Get("package"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if fps == nil {
		fps = []model.Fingerprint{}
	}
	writeJSON(w, http.StatusOK, fps)
}

const fingerprintExportSchema = "goodman.fingerprints.export/v1"

type fingerprintExportEnvelope struct {
	Schema       string              `json:"schema"`
	ExportedAt   uint64              `json:"exported_at"`
	Collector    string              `json:"collector"`
	Fingerprints []model.Fingerprint `json:"fingerprints"`
}

type fingerprintImportResult struct {
	Imported           int `json:"imported"`
	SkippedLocal       int `json:"skipped_local"`
	Replaced           int `json:"replaced"`
	IgnoredNonBaseline int `json:"ignored_non_baseline"`
}

func (s *Server) handleExportFingerprints(w http.ResponseWriter, r *http.Request) {
	fps, err := s.store.ListBaselines(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if fps == nil {
		fps = []model.Fingerprint{}
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "goodman-collector"
	}
	writeJSON(w, http.StatusOK, fingerprintExportEnvelope{
		Schema:       fingerprintExportSchema,
		ExportedAt:   uint64(time.Now().UnixNano()),
		Collector:    host,
		Fingerprints: fps,
	})
}

func (s *Server) handleImportFingerprints(w http.ResponseWriter, r *http.Request) {
	var body fingerprintExportEnvelope
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Schema != fingerprintExportSchema {
		http.Error(w, "unsupported schema: "+body.Schema, http.StatusBadRequest)
		return
	}
	var result fingerprintImportResult
	for i := range body.Fingerprints {
		outcome, err := s.store.ImportFingerprint(r.Context(), &body.Fingerprints[i])
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		switch outcome {
		case store.ImportImported:
			result.Imported++
		case store.ImportSkippedLocal:
			result.SkippedLocal++
		case store.ImportReplaced:
			result.Replaced++
		case store.ImportIgnoredNonBaseline:
			result.IgnoredNonBaseline++
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// handleReport builds the runtime reachability report from an uploaded npm
// lockfile: it joins declared dependencies against observed fingerprints and,
// when ?osv=1 is set (and the collector has egress), enriches with OSV.dev.
// The request body is the raw package-lock.json.
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	declared, err := report.ParseLockfile(body)
	if err != nil {
		http.Error(w, "parse lockfile: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(declared) == 0 {
		http.Error(w, "no packages found in lockfile", http.StatusBadRequest)
		return
	}
	fps, err := s.store.ListFingerprints(r.Context(), service, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	osv := r.URL.Query().Get("osv") == "1" || r.URL.Query().Get("osv") == "true"
	var vulns map[string][]report.Vulnerability
	if osv {
		vulns, err = report.NewOSVClient().Query(r.Context(), declared)
		if err != nil {
			http.Error(w, "osv: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	rep := report.Build(service, declared, fps, vulns)

	// persist=1 stores the lockfile and this snapshot so the dashboard can
	// load it instantly next time and the collector can recompute it on a
	// schedule as fingerprints change.
	if r.URL.Query().Get("persist") == "1" || r.URL.Query().Get("persist") == "true" {
		now := uint64(time.Now().UnixNano())
		if err := s.store.SaveLockfile(r.Context(), service, string(body), now); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		repJSON, _ := json.Marshal(rep)
		if err := s.store.SaveReport(r.Context(), service, string(repJSON), osv, now); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleGetReport returns the most recently stored reachability snapshot for a
// service scope (404 when none has been uploaded yet). This lets the dashboard
// show current numbers on load without re-uploading a lockfile. When a previous
// snapshot exists, the response includes a week-over-week delta.
func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	stored, found, err := s.store.GetReport(r.Context(), service)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "no stored report for this service", http.StatusNotFound)
		return
	}
	out := map[string]any{
		"computed_at": stored.ComputedAt,
		"osv":         stored.OSV,
		"report":      json.RawMessage(stored.Report),
	}
	if stored.PreviousReport != "" && stored.PreviousComputedAt > 0 {
		var cur, prev report.Report
		if err := json.Unmarshal([]byte(stored.Report), &cur); err == nil {
			_ = json.Unmarshal([]byte(stored.PreviousReport), &prev)
			out["previous"] = map[string]any{
				"computed_at": stored.PreviousComputedAt,
				"report":      json.RawMessage(stored.PreviousReport),
			}
			out["delta"] = report.ComputeDelta(cur, prev, stored.PreviousComputedAt)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetCoverage returns the Coverage and trust panel snapshot.
func (s *Server) handleGetCoverage(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	since := uint64(now.Add(-24 * time.Hour).UnixNano())
	n, err := s.store.CountAlertsSince(r.Context(), since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	wb, err := s.store.CountWouldBlockSince(r.Context(), since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, s.cover.Snapshot(now, n, wb))
}

// handlePostCoverage accepts a namespace injection coverage report from a sensor.
func (s *Server) handlePostCoverage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sensor     string                       `json:"sensor"`
		Namespaces []coverage.NamespaceCoverage `json:"namespaces"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Sensor == "" {
		http.Error(w, "sensor is required", http.StatusBadRequest)
		return
	}
	s.cover.SetNamespaces(body.Sensor, body.Namespaces, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"accepted": len(body.Namespaces)})
}

// handleStream is a server-sent-events feed of live events and alerts,
// consumed by `goodmanctl tail` and the dashboard's live view.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan []byte, 256)
	s.mu.Lock()
	s.subs[ch] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	fmt.Fprintf(w, ": connected\n\n")
	fl.Flush()

	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-hb.C:
			fmt.Fprintf(w, ": hb\n\n")
			fl.Flush()
		case msg := <-ch:
			w.Write(msg)
			fl.Flush()
		}
	}
}

func (s *Server) broadcast(event string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	msg := []byte("event: " + event + "\ndata: " + string(data) + "\n\n")
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- msg:
		default: // slow subscriber: drop, never block ingestion
		}
	}
}

// TLSFiles points at a PEM certificate/key pair; both empty means plain HTTP.
type TLSFiles struct {
	CertFile string
	KeyFile  string
}

// Serve runs the HTTP(S) server until ctx is cancelled. TLS is enabled when
// tls has both files set; setting only one is a configuration error.
func Serve(ctx context.Context, addr string, h http.Handler, tls TLSFiles) error {
	if (tls.CertFile == "") != (tls.KeyFile == "") {
		return fmt.Errorf("tls: certificate and key must both be set (cert=%q key=%q)", tls.CertFile, tls.KeyFile)
	}
	srv := &http.Server{Addr: addr, Handler: h}
	errCh := make(chan error, 1)
	go func() {
		if tls.CertFile != "" {
			errCh <- srv.ListenAndServeTLS(tls.CertFile, tls.KeyFile)
			return
		}
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
