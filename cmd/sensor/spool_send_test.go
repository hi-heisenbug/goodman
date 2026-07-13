package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func TestSendBatchesSpoolsAcrossCollectorOutage(t *testing.T) {
	var (
		mu       sync.Mutex
		received int
		up       atomic.Bool
	)
	up.Store(false)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		var body io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			gz, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Errorf("gzip: %v", err)
				w.WriteHeader(400)
				return
			}
			defer gz.Close()
			body = gz
		}
		var batch model.EventBatch
		if err := json.NewDecoder(body).Decode(&batch); err != nil {
			t.Errorf("decode: %v", err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		received += len(batch.Events)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	in := make(chan model.Attributed, 8)
	var dropped atomic.Uint64
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sendBatches(ctx, ts.Client(), ts.URL, "", "node-a", in, 50*time.Millisecond, 1000, &dropped)
		close(done)
	}()

	for i := 0; i < 5; i++ {
		in <- model.Attributed{Service: "web", Package: "express", Version: "1", Behavior: "READ /x", Timestamp: uint64(i + 1)}
	}
	time.Sleep(200 * time.Millisecond) // fail + spool
	mu.Lock()
	got := received
	mu.Unlock()
	if got != 0 {
		t.Fatalf("collector was down; received=%d", got)
	}

	up.Store(true)
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got = received
		mu.Unlock()
		if got >= 5 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	if got < 5 {
		t.Fatalf("after recovery received=%d, want ≥5", got)
	}
	if dropped.Load() != 0 {
		t.Fatalf("channel drops=%d", dropped.Load())
	}
}
