// Package api serves the collector's HTTP surface: sensor ingestion, operator
// APIs, SSE, Prometheus metrics, and the embedded dashboard UI.
package api

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
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
		Help: "Attributed events received from sensors.",
	})
	alertsEmitted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "goodman_collector_alerts_total",
		Help: "Alerts emitted by the diff engine.",
	}, []string{"severity"})
)

type Alerter interface {
	Notify(model.Alert)
}

type Server struct {
	store   *store.Store
	fpEng   *fingerprint.Engine
	diffEng *diff.Engine
	cover   *coverage.Registry
	enforce *enforce.Manager

	Auth      AuthConfig
	Notifier  Alerter
	OSVClient *report.OSVClient

	mu   sync.Mutex
	subs map[chan []byte]bool
}

func NewServer(store *store.Store, fpEng *fingerprint.Engine, diffEng *diff.Engine) *Server {
	return &Server{
		store: store, fpEng: fpEng, diffEng: diffEng,
		cover:     coverage.NewRegistry(),
		subs:      map[chan []byte]bool{},
		OSVClient: report.NewOSVClient(),
	}
}

func (s *Server) SetEnforceManager(manager *enforce.Manager) {
	s.enforce = manager
}

func (s *Server) SetAlertBudget(budget int) {
	if s.cover != nil {
		s.cover.SetAlertBudget(budget)
	}
}

func (s *Server) Router(ui fs.FS) http.Handler {
	return s.router(ui, 60*time.Second)
}

func (s *Server) router(ui fs.FS, requestTimeout time.Duration) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.Recoverer)
	router.Get("/v1/stream", requireToken(s.Auth.APIToken, true, s.handleStream))
	router.Group(func(timed chi.Router) {
		if requestTimeout > 0 {
			timed.Use(middleware.Timeout(requestTimeout))
		}
		s.registerAPIRoutes(timed)
		if ui != nil {
			s.registerUI(timed, ui)
		}
	})
	return router
}

func (s *Server) registerAPIRoutes(router chi.Router) {
	router.Get("/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/v1/readyz", s.handleReadyz)
	router.Post("/v1/events", requireToken(s.Auth.IngestToken, false, s.handleIngest))
	router.Post("/v1/coverage", requireToken(s.Auth.IngestToken, false, s.handlePostCoverage))
	router.Get("/v1/coverage", requireToken(s.Auth.APIToken, false, s.handleGetCoverage))
	router.Get("/v1/alerts", requireToken(s.Auth.APIToken, false, s.handleListAlerts))
	router.Post("/v1/alerts/{id}/ack", requireToken(s.Auth.APIToken, false, s.alertStatusHandler(model.AlertAcknowledged)))
	router.Post("/v1/alerts/{id}/resolve", requireToken(s.Auth.APIToken, false, s.alertStatusHandler(model.AlertResolved)))
	router.Get("/v1/snapshot", requireToken(s.Auth.APIToken, false, s.handleSnapshot))
	router.Get("/v1/export", requireToken(s.Auth.APIToken, false, s.handleExport))
	router.Get("/v1/fingerprints", requireToken(s.Auth.APIToken, false, s.handleListFingerprints))
	router.Get("/v1/fingerprints/export", requireToken(s.Auth.APIToken, false, s.handleExportFingerprints))
	router.Post("/v1/fingerprints/import", requireToken(s.Auth.APIToken, false, s.handleImportFingerprints))
	router.Post("/v1/report", requireToken(s.Auth.APIToken, false, s.handleReport))
	router.Get("/v1/report", requireToken(s.Auth.APIToken, false, s.handleGetReport))
	router.Get("/v1/enforce/state", requireToken(s.Auth.IngestToken, false, s.handleEnforceState))
	router.Get("/v1/enforce", requireToken(s.Auth.APIToken, false, s.handleEnforceStatus))
	router.Post("/v1/enforce/on", requireToken(s.Auth.APIToken, false, s.handleEnforceOn))
	router.Post("/v1/enforce/off", requireToken(s.Auth.APIToken, false, s.handleEnforceOff))
	router.Handle("/metrics", requireToken(s.Auth.APIToken, false, promhttp.Handler().ServeHTTP))
}

func (s *Server) registerUI(router chi.Router, ui fs.FS) {
	fileServer := http.FileServer(http.FS(ui))
	router.Get("/*", func(w http.ResponseWriter, request *http.Request) {
		path := strings.TrimPrefix(request.URL.Path, "/")
		if path != "" {
			if file, err := ui.Open(path); err == nil {
				_ = file.Close()
				fileServer.ServeHTTP(w, request)
				return
			}
		}
		request.URL.Path = "/"
		fileServer.ServeHTTP(w, request)
	})
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func writeInternalError(w http.ResponseWriter, operation string, err error) {
	log.Printf("api: %s: %v", operation, err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
