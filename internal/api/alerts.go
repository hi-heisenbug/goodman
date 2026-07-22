package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/store"
)

func (s *Server) handleListAlerts(w http.ResponseWriter, request *http.Request) {
	limit, err := queryInt(request, "limit", 100)
	if err != nil || limit < 1 || limit > 500 {
		http.Error(w, "limit must be between 1 and 500", http.StatusBadRequest)
		return
	}
	offset, err := queryInt(request, "offset", 0)
	if err != nil || offset < 0 {
		http.Error(w, "offset must be a non-negative integer", http.StatusBadRequest)
		return
	}
	alerts, err := s.store.ListAlertsPage(request.Context(), request.URL.Query().Get("status"), limit, offset)
	if err != nil {
		writeInternalError(w, "list alerts", err)
		return
	}
	if err := s.enrichAlerts(request.Context(), alerts); err != nil {
		writeInternalError(w, "enrich alerts", err)
		return
	}
	if alerts == nil {
		alerts = []model.Alert{}
	}
	writeJSON(w, http.StatusOK, alerts)
}

func queryInt(request *http.Request, name string, fallback int) (int, error) {
	value := request.URL.Query().Get(name)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func (s *Server) enrichAlerts(ctx context.Context, alerts []model.Alert) error {
	keys := make([]store.FingerprintKey, 0, len(alerts))
	wanted := make(map[string]bool, len(alerts))
	for i := range alerts {
		if alerts[i].OldVersion == "" || len(alerts[i].BaselineBehaviors) > 0 {
			continue
		}
		key := fingerprintLookupKey(alerts[i].Service, alerts[i].Package, alerts[i].OldVersion)
		if wanted[key] {
			continue
		}
		wanted[key] = true
		keys = append(keys, store.FingerprintKey{
			Service: alerts[i].Service, Package: alerts[i].Package, Version: alerts[i].OldVersion,
		})
	}
	fingerprints, err := s.store.GetFingerprints(ctx, keys)
	if err != nil {
		return err
	}
	baselines := make(map[string][]string, len(fingerprints))
	for i := range fingerprints {
		baselines[fingerprintLookupKey(fingerprints[i].Service, fingerprints[i].Package, fingerprints[i].Version)] = behaviorKeys(fingerprints[i].Behaviors)
	}
	for i := range alerts {
		if len(alerts[i].BaselineBehaviors) == 0 {
			alerts[i].BaselineBehaviors = baselines[fingerprintLookupKey(alerts[i].Service, alerts[i].Package, alerts[i].OldVersion)]
		}
	}
	return nil
}

func fingerprintLookupKey(service, pkg, version string) string {
	return service + "\x00" + pkg + "\x00" + version
}

func behaviorKeys(behaviors map[string]model.BehaviorStat) []string {
	keys := make([]string, 0, len(behaviors))
	for behavior := range behaviors {
		keys = append(keys, behavior)
	}
	sort.Strings(keys)
	return keys
}

func (s *Server) alertStatusHandler(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		err := s.store.SetAlertStatus(request.Context(), chi.URLParam(request, "id"), status)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "no such alert", http.StatusNotFound)
			return
		}
		if err != nil {
			writeInternalError(w, "set alert status", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": status})
	}
}
