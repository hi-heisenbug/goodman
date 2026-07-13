package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func alert(sev string) model.Alert {
	return model.Alert{
		ID: "a1", Service: "web", Package: "good-pkg",
		OldVersion: "1.0.0", NewVersion: "1.0.1",
		Severity:     sev,
		NewBehaviors: []string{"READ /etc/secret"},
		DetectedAt:   uint64(time.Now().UnixNano()),
		Status:       model.AlertOpen,
	}
}

func runOne(t *testing.T, n *Notifier, a model.Alert) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { n.Run(ctx); close(done) }()
	n.Notify(a)
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done
}

func TestGenericDelivery(t *testing.T) {
	var got atomic.Value
	var auth atomic.Value
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.Store(string(b))
		auth.Store(r.Header.Get("Authorization"))
	}))
	defer ts.Close()

	n, err := New(Config{URL: ts.URL, Token: "hook-secret"})
	if err != nil {
		t.Fatal(err)
	}
	runOne(t, n, alert(model.SeverityCritical))

	body, _ := got.Load().(string)
	if body == "" {
		t.Fatal("webhook was not called")
	}
	var payload struct {
		Type  string      `json:"type"`
		Alert model.Alert `json:"alert"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if payload.Type != "goodman.alert" || payload.Alert.Package != "good-pkg" {
		t.Fatalf("unexpected payload: %s", body)
	}
	if a, _ := auth.Load().(string); a != "Bearer hook-secret" {
		t.Fatalf("Authorization = %q", a)
	}
}

func TestSlackFormatAndSeverityFilter(t *testing.T) {
	var calls atomic.Int64
	var got atomic.Value
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		got.Store(string(b))
	}))
	defer ts.Close()

	n, err := New(Config{
		URL: ts.URL, Format: FormatSlack, MinSeverity: model.SeverityCritical,
		PublicURL: "https://goodman.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { n.Run(ctx); close(done) }()
	n.Notify(alert(model.SeverityWarn)) // below threshold: filtered
	a := alert(model.SeverityCritical)
	a.MatchedRules = []string{"secret-read", "new-outbound-connect"}
	a.Evidence = []model.Evidence{{
		Behavior:  "READ /etc/secret",
		Rules:     []string{"secret-read"},
		Sensor:    "node-a",
		FirstSeen: uint64(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC).UnixNano()),
	}}
	n.Notify(a)
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	if c := calls.Load(); c != 1 {
		t.Fatalf("webhook calls = %d, want 1 (WARN must be filtered)", c)
	}
	body, _ := got.Load().(string)
	for _, needle := range []string{
		`"text"`, "good-pkg", "secret-read", "sensor `node-a`",
		"first seen", "Open in Goodman", "https://goodman.example/#alerts?id=a1",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("Slack payload missing %q: %s", needle, body)
		}
	}
}

func TestRetryOnFailure(t *testing.T) {
	var calls atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer ts.Close()

	n, err := New(Config{URL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	n.backoff = func(int) time.Duration { return time.Millisecond }
	runOne(t, n, alert(model.SeverityCritical))

	if c := calls.Load(); c != 2 {
		t.Fatalf("webhook calls = %d, want 2 (one failure, one retry)", c)
	}
}

func TestConfigValidation(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("empty URL must error")
	}
	if _, err := New(Config{URL: "http://x", Format: "teams"}); err == nil {
		t.Fatal("unknown format must error")
	}
	if _, err := New(Config{URL: "http://x", MinSeverity: "LOUD"}); err == nil {
		t.Fatal("unknown severity must error")
	}
}
