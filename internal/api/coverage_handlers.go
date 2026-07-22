package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/hi-heisenbug/goodman/internal/coverage"
)

func (s *Server) handleGetCoverage(w http.ResponseWriter, request *http.Request) {
	snapshot, err := s.coverageSnapshot(request.Context(), time.Now())
	if err != nil {
		writeInternalError(w, "coverage snapshot", err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) coverageSnapshot(ctx context.Context, now time.Time) (coverage.Snapshot, error) {
	since := uint64(now.Add(-24 * time.Hour).UnixNano())
	alerts, err := s.store.CountAlertsSince(ctx, since)
	if err != nil {
		return coverage.Snapshot{}, err
	}
	wouldBlock, err := s.store.CountWouldBlockSince(ctx, since)
	if err != nil {
		return coverage.Snapshot{}, err
	}
	return s.cover.Snapshot(now, alerts, wouldBlock), nil
}

func (s *Server) handlePostCoverage(w http.ResponseWriter, request *http.Request) {
	var body struct {
		Sensor     string                       `json:"sensor"`
		Namespaces []coverage.NamespaceCoverage `json:"namespaces"`
	}
	if err := json.NewDecoder(io.LimitReader(request.Body, 4<<20)).Decode(&body); err != nil {
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
