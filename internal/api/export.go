package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hi-heisenbug/goodman/internal/coverage"
	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/store"
)

const snapshotSchema = "goodman.snapshot/v1"

type snapshotEnvelope struct {
	Schema       string              `json:"schema"`
	GeneratedAt  uint64              `json:"generated_at"`
	Alerts       []model.Alert       `json:"alerts"`
	Fingerprints []model.Fingerprint `json:"fingerprints"`
}

const exportSchema = "goodman.export/v1"

type exportReachability struct {
	Service    string          `json:"service"`
	ComputedAt uint64          `json:"computed_at"`
	OSV        bool            `json:"osv"`
	Report     json.RawMessage `json:"report"`
	Previous   *struct {
		ComputedAt uint64          `json:"computed_at"`
		Report     json.RawMessage `json:"report"`
	} `json:"previous,omitempty"`
}

type exportEnvelope struct {
	Schema       string                   `json:"schema"`
	GeneratedAt  uint64                   `json:"generated_at"`
	Alerts       []model.Alert            `json:"alerts"`
	Fingerprints []model.Fingerprint      `json:"fingerprints"`
	Reachability []exportReachability     `json:"reachability"`
	Coverage     coverage.Snapshot        `json:"coverage"`
	Enforcement  map[string]any           `json:"enforcement"`
	Live         exportLiveStreamContract `json:"live"`
}

type exportLiveStreamContract struct {
	EventsPersisted bool   `json:"events_persisted"`
	Stream          string `json:"stream"`
	Delivery        string `json:"delivery"`
}

func (s *Server) handleSnapshot(w http.ResponseWriter, request *http.Request) {
	alerts, err := s.store.ListAlertsPage(request.Context(), model.AlertOpen, 500, 0)
	if err != nil {
		writeInternalError(w, "snapshot alerts", err)
		return
	}
	if err := s.enrichAlerts(request.Context(), alerts); err != nil {
		writeInternalError(w, "snapshot alert baselines", err)
		return
	}
	fingerprints, err := s.store.ListFingerprints(request.Context(), "", "")
	if err != nil {
		writeInternalError(w, "snapshot fingerprints", err)
		return
	}
	writeJSON(w, http.StatusOK, snapshotEnvelope{
		Schema: snapshotSchema, GeneratedAt: uint64(time.Now().UnixNano()),
		Alerts: nonNilAlerts(alerts), Fingerprints: nonNilFingerprints(fingerprints),
	})
}

func (s *Server) handleExport(w http.ResponseWriter, request *http.Request) {
	alerts, err := s.listAllAlerts(request.Context())
	if err != nil {
		writeInternalError(w, "export alerts", err)
		return
	}
	fingerprints, err := s.store.ListFingerprints(request.Context(), "", "")
	if err != nil {
		writeInternalError(w, "export fingerprints", err)
		return
	}
	reports, err := s.store.ListReports(request.Context())
	if err != nil {
		writeInternalError(w, "export reachability", err)
		return
	}
	coverageSnapshot, err := s.coverageSnapshot(request.Context(), time.Now())
	if err != nil {
		writeInternalError(w, "export coverage", err)
		return
	}
	writeJSON(w, http.StatusOK, exportEnvelope{
		Schema: exportSchema, GeneratedAt: uint64(time.Now().UnixNano()),
		Alerts: nonNilAlerts(alerts), Fingerprints: nonNilFingerprints(fingerprints),
		Reachability: exportReports(reports), Coverage: coverageSnapshot,
		Enforcement: s.exportEnforcement(),
		Live: exportLiveStreamContract{
			EventsPersisted: false, Stream: "/v1/stream",
			Delivery: "best-effort SSE; use alert webhooks for retrying push delivery",
		},
	})
}

func nonNilAlerts(alerts []model.Alert) []model.Alert {
	if alerts == nil {
		return []model.Alert{}
	}
	return alerts
}

func nonNilFingerprints(fingerprints []model.Fingerprint) []model.Fingerprint {
	if fingerprints == nil {
		return []model.Fingerprint{}
	}
	return fingerprints
}

func exportReports(reports []store.StoredReachability) []exportReachability {
	out := make([]exportReachability, 0, len(reports))
	for _, stored := range reports {
		item := exportReachability{
			Service: stored.Service, ComputedAt: stored.ComputedAt, OSV: stored.OSV,
			Report: json.RawMessage(stored.Report),
		}
		if stored.PreviousReport != "" && stored.PreviousComputedAt > 0 {
			item.Previous = &struct {
				ComputedAt uint64          `json:"computed_at"`
				Report     json.RawMessage `json:"report"`
			}{ComputedAt: stored.PreviousComputedAt, Report: json.RawMessage(stored.PreviousReport)}
		}
		out = append(out, item)
	}
	return out
}

func (s *Server) listAllAlerts(ctx context.Context) ([]model.Alert, error) {
	const pageSize = 500
	var alerts []model.Alert
	for offset := 0; ; offset += pageSize {
		page, err := s.store.ListAlertsPage(ctx, "", pageSize, offset)
		if err != nil {
			return nil, err
		}
		if err := s.enrichAlerts(ctx, page); err != nil {
			return nil, err
		}
		alerts = append(alerts, page...)
		if len(page) < pageSize {
			return alerts, nil
		}
	}
}

func (s *Server) exportEnforcement() map[string]any {
	if s.enforce == nil {
		return map[string]any{"master_gate": false, "enabled": false}
	}
	enabled, master, rev, verdicts, sensors := s.enforce.Status()
	return map[string]any{
		"master_gate": master, "enabled": enabled, "rev": rev,
		"verdicts": verdicts, "skipped": skippedVerdicts(verdicts), "sensors": sensors,
	}
}
