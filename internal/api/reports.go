package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/hi-heisenbug/goodman/internal/report"
	"github.com/hi-heisenbug/goodman/internal/store"
)

func (s *Server) handleReport(w http.ResponseWriter, request *http.Request) {
	service := request.URL.Query().Get("service")
	body, err := io.ReadAll(io.LimitReader(request.Body, 32<<20))
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
	fingerprints, err := s.store.ListFingerprints(request.Context(), service, "")
	if err != nil {
		writeInternalError(w, "list report fingerprints", err)
		return
	}
	osv := queryBool(request, "osv")
	var vulnerabilities map[string][]report.Vulnerability
	if osv {
		vulnerabilities, err = s.OSVClient.Query(request.Context(), declared)
		if err != nil {
			http.Error(w, "osv: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	reachability := report.Build(service, declared, fingerprints, vulnerabilities)
	if queryBool(request, "persist") {
		if operation, err := s.persistReport(request, service, body, reachability, osv); err != nil {
			writeInternalError(w, operation, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, reachability)
}

func queryBool(request *http.Request, name string) bool {
	value := request.URL.Query().Get(name)
	return value == "1" || value == "true"
}

func (s *Server) persistReport(request *http.Request, service string, lockfile []byte, reachability report.Report, osv bool) (string, error) {
	now := uint64(time.Now().UnixNano())
	if err := s.store.SaveLockfile(request.Context(), service, string(lockfile), now); err != nil {
		return "save lockfile", err
	}
	reportJSON, _ := json.Marshal(reachability)
	if err := s.store.SaveReport(request.Context(), service, string(reportJSON), osv, now); err != nil {
		return "save reachability report", err
	}
	return "", nil
}

func (s *Server) handleGetReport(w http.ResponseWriter, request *http.Request) {
	service := request.URL.Query().Get("service")
	stored, found, err := s.store.GetReport(request.Context(), service)
	if err != nil {
		writeInternalError(w, "get reachability report", err)
		return
	}
	if !found && service == "" {
		stored, found, err = s.fallbackReport(request)
		if err != nil {
			writeInternalError(w, "get fallback reachability report", err)
			return
		}
	}
	if !found {
		http.Error(w, "no stored report for this service", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, storedReportResponse(stored))
}

func (s *Server) fallbackReport(request *http.Request) (store.StoredReachability, bool, error) {
	lockfiles, err := s.store.ListLockfiles(request.Context())
	if err != nil {
		return store.StoredReachability{}, false, err
	}
	for _, lockfile := range lockfiles {
		if lockfile.Service == "" {
			continue
		}
		stored, found, err := s.store.GetReport(request.Context(), lockfile.Service)
		if err != nil || found {
			return stored, found, err
		}
	}
	return store.StoredReachability{}, false, nil
}

func storedReportResponse(stored store.StoredReachability) map[string]any {
	out := map[string]any{
		"computed_at": stored.ComputedAt,
		"osv":         stored.OSV,
		"report":      json.RawMessage(stored.Report),
	}
	if stored.PreviousReport == "" || stored.PreviousComputedAt == 0 {
		return out
	}
	var current, previous report.Report
	if err := json.Unmarshal([]byte(stored.Report), &current); err != nil {
		return out
	}
	_ = json.Unmarshal([]byte(stored.PreviousReport), &previous)
	out["previous"] = map[string]any{
		"computed_at": stored.PreviousComputedAt,
		"report":      json.RawMessage(stored.PreviousReport),
	}
	out["delta"] = report.ComputeDelta(current, previous, stored.PreviousComputedAt)
	return out
}
