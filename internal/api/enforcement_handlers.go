package api

import (
	"errors"
	"net/http"

	"github.com/hi-heisenbug/goodman/internal/enforce"
)

func (s *Server) handleEnforceState(w http.ResponseWriter, request *http.Request) {
	sensor := request.URL.Query().Get("sensor")
	active := request.URL.Query().Get("enforcement_active") == "true"
	if s.enforce == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "rev": 0, "verdicts": enforce.ServiceVerdicts{}})
		return
	}
	if sensor != "" {
		s.enforce.RecordSensorHeartbeat(sensor, active)
	}
	enabled, rev, verdicts := s.enforce.StateForSensor()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": enabled, "rev": rev, "verdicts": verdicts,
		"skipped": skippedVerdicts(verdicts),
	})
}

func skippedVerdicts(verdicts enforce.ServiceVerdicts) map[string][]enforce.SkippedVerdict {
	out := make(map[string][]enforce.SkippedVerdict)
	for service, serviceVerdicts := range verdicts {
		if len(serviceVerdicts.Skipped) > 0 {
			out[service] = serviceVerdicts.Skipped
		}
	}
	return out
}

func (s *Server) handleEnforceStatus(w http.ResponseWriter, _ *http.Request) {
	if s.enforce == nil {
		writeJSON(w, http.StatusOK, map[string]any{"master_gate": false, "enabled": false})
		return
	}
	enabled, master, rev, verdicts, sensors := s.enforce.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"master_gate": master, "enabled": enabled, "rev": rev,
		"verdicts": verdicts, "skipped": skippedVerdicts(verdicts), "sensors": sensors,
	})
}

func (s *Server) handleEnforceOn(w http.ResponseWriter, request *http.Request) {
	if s.enforce == nil {
		http.Error(w, "enforcement not configured", http.StatusNotFound)
		return
	}
	if err := s.enforce.SetEnabled(request.Context(), true); err != nil {
		if errors.Is(err, enforce.ErrMasterGateOff) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeInternalError(w, "enable enforcement", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true})
}

func (s *Server) handleEnforceOff(w http.ResponseWriter, request *http.Request) {
	if s.enforce == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	if err := s.enforce.SetEnabled(request.Context(), false); err != nil {
		writeInternalError(w, "disable enforcement", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
}
