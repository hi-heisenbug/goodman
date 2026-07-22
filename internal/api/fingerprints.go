package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/store"
)

func (s *Server) handleListFingerprints(w http.ResponseWriter, request *http.Request) {
	fingerprints, err := s.store.ListFingerprints(request.Context(),
		request.URL.Query().Get("service"), request.URL.Query().Get("package"))
	if err != nil {
		writeInternalError(w, "list fingerprints", err)
		return
	}
	writeJSON(w, http.StatusOK, nonNilFingerprints(fingerprints))
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

func (s *Server) handleExportFingerprints(w http.ResponseWriter, request *http.Request) {
	fingerprints, err := s.store.ListBaselines(request.Context())
	if err != nil {
		writeInternalError(w, "export fingerprints", err)
		return
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "goodman-collector"
	}
	writeJSON(w, http.StatusOK, fingerprintExportEnvelope{
		Schema: fingerprintExportSchema, ExportedAt: uint64(time.Now().UnixNano()),
		Collector: host, Fingerprints: nonNilFingerprints(fingerprints),
	})
}

func (s *Server) handleImportFingerprints(w http.ResponseWriter, request *http.Request) {
	var body fingerprintExportEnvelope
	if err := json.NewDecoder(io.LimitReader(request.Body, 64<<20)).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Schema != fingerprintExportSchema {
		http.Error(w, "unsupported schema: "+body.Schema, http.StatusBadRequest)
		return
	}
	var result fingerprintImportResult
	for i := range body.Fingerprints {
		outcome, err := s.store.ImportFingerprint(request.Context(), &body.Fingerprints[i])
		if err != nil {
			writeInternalError(w, "import fingerprint", err)
			return
		}
		countImportOutcome(&result, outcome)
	}
	writeJSON(w, http.StatusOK, result)
}

func countImportOutcome(result *fingerprintImportResult, outcome store.ImportOutcome) {
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
