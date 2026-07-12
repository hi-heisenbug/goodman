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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hi-heisenbug/goodman/internal/diff"
	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/model"
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

	// Auth protects the HTTP surface; zero value leaves it open (local dev).
	Auth AuthConfig
	// Notifier, when set, receives every emitted alert (webhook delivery).
	Notifier Alerter

	mu   sync.Mutex
	subs map[chan []byte]bool // SSE subscribers
}

func NewServer(s *store.Store, fpEng *fingerprint.Engine, diffEng *diff.Engine) *Server {
	return &Server{store: s, fpEng: fpEng, diffEng: diffEng, subs: map[chan []byte]bool{}}
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
	r.Get("/v1/alerts", requireToken(s.Auth.APIToken, false, s.handleListAlerts))
	r.Post("/v1/alerts/{id}/ack", requireToken(s.Auth.APIToken, false, s.alertStatusHandler(model.AlertAcknowledged)))
	r.Post("/v1/alerts/{id}/resolve", requireToken(s.Auth.APIToken, false, s.alertStatusHandler(model.AlertResolved)))
	r.Get("/v1/fingerprints", requireToken(s.Auth.APIToken, false, s.handleListFingerprints))
	// EventSource cannot set headers, so the stream also accepts ?token=.
	r.Get("/v1/stream", requireToken(s.Auth.APIToken, true, s.handleStream))
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

	ctx := r.Context()
	updates, err := s.fpEng.Ingest(ctx, batch.Events)
	if err != nil {
		log.Printf("api: ingest: %v", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	var alerts []model.Alert
	for _, up := range updates {
		a, err := s.diffEng.React(ctx, up)
		if err != nil {
			log.Printf("api: diff: %v", err)
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
