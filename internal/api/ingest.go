package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/model"
)

func (s *Server) handleReadyz(w http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleIngest(w http.ResponseWriter, request *http.Request) {
	body, closeBody, err := ingestBody(request)
	if err != nil {
		http.Error(w, "bad gzip", http.StatusBadRequest)
		return
	}
	defer closeBody()
	var batch model.EventBatch
	if err := json.NewDecoder(io.LimitReader(body, 64<<20)).Decode(&batch); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	normalizeBatch(&batch)
	if s.cover != nil {
		s.cover.ObserveIngest(batch.Sensor, batch.Events, time.Now())
	}
	if len(batch.Events) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ingested": 0, "alerts": 0})
		return
	}
	alerts, err := s.processEvents(request.Context(), batch.Events)
	if err != nil {
		log.Printf("api: ingest: %v", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	s.broadcast("events", batch.Events)
	if len(alerts) > 0 {
		s.broadcast("alerts", alerts)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ingested": len(batch.Events), "alerts": len(alerts)})
}

func ingestBody(request *http.Request) (io.Reader, func(), error) {
	if request.Header.Get("Content-Encoding") != "gzip" {
		return request.Body, func() {}, nil
	}
	reader, err := gzip.NewReader(request.Body)
	if err != nil {
		return nil, nil, err
	}
	return reader, func() { _ = reader.Close() }, nil
}

func normalizeBatch(batch *model.EventBatch) {
	eventsIngested.Add(float64(len(batch.Events)))
	for i := range batch.Events {
		if batch.Events[i].Sensor == "" {
			batch.Events[i].Sensor = batch.Sensor
		}
	}
}

func (s *Server) processEvents(ctx context.Context, events []model.Attributed) ([]model.Alert, error) {
	learn, denied := partitionEvents(events)
	updates, err := s.fpEng.Ingest(ctx, learn)
	if err != nil {
		return nil, err
	}
	alerts := s.reactToUpdates(ctx, updates)
	return append(alerts, s.reactToDenied(ctx, denied)...), nil
}

func partitionEvents(events []model.Attributed) (learn, denied []model.Attributed) {
	for _, event := range events {
		if event.Denied {
			denied = append(denied, event)
			continue
		}
		learn = append(learn, event)
	}
	return learn, denied
}

func (s *Server) reactToUpdates(ctx context.Context, updates []fingerprint.Update) []model.Alert {
	var alerts []model.Alert
	for _, update := range updates {
		for _, behavior := range update.FreshBehaviors {
			s.recordBlockBehavior(update.Fingerprint.Service, behavior)
		}
		alert, err := s.diffEng.React(ctx, update)
		if err != nil {
			log.Printf("api: diff: %v", err)
			continue
		}
		if alert == nil {
			continue
		}
		for _, behavior := range alert.NewBehaviors {
			s.recordBlockBehavior(alert.Service, behavior)
		}
		alerts = s.recordAlert(alerts, alert)
	}
	return alerts
}

func (s *Server) reactToDenied(ctx context.Context, denied []model.Attributed) []model.Alert {
	var alerts []model.Alert
	for _, event := range denied {
		s.recordBlockBehavior(event.Service, event.Behavior)
		alert, err := s.diffEng.ReactDenied(ctx, event)
		if err != nil {
			log.Printf("api: denied: %v", err)
			continue
		}
		if alert != nil {
			alerts = s.recordAlert(alerts, alert)
		}
	}
	return alerts
}

func (s *Server) recordAlert(alerts []model.Alert, alert *model.Alert) []model.Alert {
	alerts = append(alerts, *alert)
	alertsEmitted.WithLabelValues(alert.Severity).Inc()
	if s.Notifier != nil {
		s.Notifier.Notify(*alert)
	}
	return alerts
}

func (s *Server) recordBlockBehavior(service, behavior string) {
	if s.enforce != nil && service != "" && behavior != "" {
		s.enforce.RecordBehavior(service, behavior)
	}
}
