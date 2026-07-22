// Collector TLS, batching, compression, and outage spooling.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/spool"
)

// newCollectorClient builds the HTTP client for collector POSTs; a non-empty
// caFile pins trust to that CA bundle (self-signed / private-CA deployments).
func newCollectorClient(caFile string) (*http.Client, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	if caFile == "" {
		return client, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no certificates found in %s", caFile)
	}
	client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	return client, nil
}

type batchSender struct {
	client  *http.Client
	url     string
	token   string
	sensor  string
	pending *spool.Spool
}

func sendBatches(ctx context.Context, client *http.Client, baseURL, token, sensor string, in <-chan model.Attributed, every time.Duration, spoolCap int, dropped *atomic.Uint64) {
	sender := batchSender{
		client: client, url: baseURL + "/v1/events", token: token,
		sensor: sensor, pending: spool.New(spoolCap),
	}
	var buffer []model.Attributed
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			sender.flush(buffer)
			if d := dropped.Load(); d > 0 {
				log.Printf("shutdown: %d events were dropped on the send buffer", d)
			}
			return
		case attributed, ok := <-in:
			if !ok {
				sender.flush(buffer)
				return
			}
			buffer = append(buffer, attributed)
			if len(buffer) >= 5000 {
				buffer = sender.flush(buffer)
			}
		case <-ticker.C:
			buffer = sender.flush(buffer)
		}
	}
}

func (s *batchSender) flush(buffer []model.Attributed) []model.Attributed {
	events := append(s.pending.TakeAll(), buffer...)
	if len(events) == 0 {
		s.recordResult(s.post(nil))
		mSpoolDepth.Set(float64(s.pending.Len()))
		return nil
	}
	if err := s.post(events); err != nil {
		s.recordFailure(events, err)
	} else {
		s.recordResult(nil)
	}
	mSpoolDepth.Set(float64(s.pending.Len()))
	return nil
}

func (s *batchSender) post(events []model.Attributed) error {
	var body bytes.Buffer
	gzipWriter := gzip.NewWriter(&body)
	if err := json.NewEncoder(gzipWriter).Encode(model.EventBatch{Sensor: s.sensor, Events: events}); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, s.url, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("collector returned %s", resp.Status)
	}
	return nil
}

func (s *batchSender) recordFailure(events []model.Attributed, err error) {
	s.recordResult(err)
	log.Printf("send batch (%d events): %v — spooling", len(events), err)
	if evicted := s.pending.Push(events); evicted > 0 {
		mSpoolDropped.Add(float64(evicted))
		log.Printf("spool full: evicted %d oldest events", evicted)
	}
}

func (s *batchSender) recordResult(err error) {
	if err != nil {
		mBatches.WithLabelValues("error").Inc()
		return
	}
	mBatches.WithLabelValues("ok").Inc()
}
