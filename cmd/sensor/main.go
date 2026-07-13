// The Goodman sensor: loads the eBPF programs, attributes each captured
// syscall to an npm package via the user-space stack, and ships batches to
// the collector. Runs as a privileged DaemonSet in k8s or as root locally.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hi-heisenbug/goodman/internal/attribute"
	"github.com/hi-heisenbug/goodman/internal/coverage"
	"github.com/hi-heisenbug/goodman/internal/loader"
	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/spool"
)

var (
	mEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "goodman_sensor_events_total", Help: "Raw events read from the kernel."}, []string{"type"})
	mAttributed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "goodman_sensor_attributed_total",
		Help: "Events by attribution outcome (package|app|unknown)."}, []string{"outcome"})
	mChanDrops = promauto.NewCounter(prometheus.CounterOpts{
		Name: "goodman_sensor_channel_drops_total",
		Help: "Events dropped because the send buffer was full."})
	mKernelDrops = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "goodman_sensor_ringbuf_drops_total",
		Help: "Events dropped in-kernel because the ring buffer was full."})
	mWatched = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "goodman_sensor_watched_pids", Help: "Currently watched pids."})
	mBatches = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "goodman_sensor_batches_total", Help: "Batch POSTs to the collector."}, []string{"result"})
	mSpoolDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "goodman_sensor_spool_dropped_total",
		Help: "Events evicted from the collector-retry spool when it was full."})
	mSpoolDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "goodman_sensor_spool_depth",
		Help: "Events waiting in the collector-retry spool."})
)

func main() {
	var (
		collectorURL  = flag.String("collector", envOr("GOODMAN_COLLECTOR_URL", "http://127.0.0.1:8844"), "collector base URL")
		procRoot      = flag.String("proc-root", envOr("GOODMAN_PROC_ROOT", "/proc"), "proc mount of the host ('/host/proc' in k8s)")
		stdout        = flag.Bool("stdout", false, "print attributed events to stdout instead of sending to the collector")
		rawStdout     = flag.Bool("raw", false, "with -stdout: also print raw events incl. stack addresses")
		batchEvery    = flag.Duration("batch-interval", envDurOr("GOODMAN_BATCH_INTERVAL", 1500*time.Millisecond), "collector flush interval")
		metricsAddr   = flag.String("metrics-addr", envOr("GOODMAN_METRICS_ADDR", ":9478"), "Prometheus metrics listen address ('' to disable)")
		extraComms    = flag.String("comms", os.Getenv("GOODMAN_EXTRA_COMMS"), "extra comm names to watch, comma-separated")
		watchInterval = flag.Duration("watch-interval", 3*time.Second, "how often to rescan /proc for runtime processes")
		ingestToken   = flag.String("ingest-token", os.Getenv("GOODMAN_INGEST_TOKEN"), "bearer token sent with event batches")
		tlsCA         = flag.String("tls-ca", os.Getenv("GOODMAN_TLS_CA"), "PEM CA bundle to trust for an https collector (empty = system roots)")
		connectCIDR   = flag.Int("connect-cidr", envIntOr("GOODMAN_CONNECT_CIDR", 0), "aggregate public destination IPs to this IPv4 prefix in CONNECT behaviors (8-32; 0 = exact IPs)")
		spoolEvents   = flag.Int("spool-events", envIntOr("GOODMAN_SPOOL_EVENTS", 50_000), "max attributed events to retain when the collector is unreachable")
	)
	flag.Parse()
	log.SetPrefix("sensor: ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	l, err := loader.New(*procRoot)
	if err != nil {
		log.Fatalf("load eBPF: %v", err)
	}
	defer l.Close()
	log.Printf("eBPF programs attached (open/openat/openat2, connect, execve); proc root %s", *procRoot)

	if *metricsAddr != "" {
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.Handler())
			if err := http.ListenAndServe(*metricsAddr, mux); err != nil {
				log.Printf("metrics server: %v", err)
			}
		}()
	}

	resolver := attribute.NewResolver(*procRoot)
	resolver.ConnectCIDRBits = *connectCIDR
	bootOffset := loader.BootToUnixNs()

	// pid watcher
	comms := strings.Split(*extraComms, ",")
	refresh := func() {
		err := l.RefreshWatched(comms,
			func(pid uint32) { log.Printf("watching pid %d", pid) },
			func(pid uint32) { resolver.Forget(int(pid)); log.Printf("pid %d exited", pid) })
		if err != nil {
			log.Printf("refresh watched pids: %v", err)
		}
		mWatched.Set(float64(len(l.Watched())))
		mKernelDrops.Set(float64(l.Drops()))
	}
	refresh()
	go func() {
		t := time.NewTicker(*watchInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				refresh()
			}
		}
	}()

	// Never block the ring-buffer reader on the network: hand off through a
	// buffered channel and count drops when it is full.
	out := make(chan model.Attributed, 8192)
	var dropped atomic.Uint64

	go func() {
		<-ctx.Done()
		l.Close() // unblocks the blocking Read below
	}()

	go func() {
		for {
			ev, err := l.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) || ctx.Err() != nil {
					close(out)
					return
				}
				log.Printf("ringbuf read: %v", err)
				continue
			}
			mEvents.WithLabelValues(model.EventType(ev.Type).String()).Inc()
			at := resolver.Attribute(ev, bootOffset)
			switch {
			case at.Package == "<unknown>":
				mAttributed.WithLabelValues("unknown").Inc()
			case at.Package == "<app>":
				mAttributed.WithLabelValues("app").Inc()
			default:
				mAttributed.WithLabelValues("package").Inc()
			}
			if *stdout && *rawStdout {
				fmt.Printf("RAW pid=%d comm=%s type=%s arg=%q stack=%d frames\n",
					ev.PID, ev.CommString(), model.EventType(ev.Type), ev.ArgString(), ev.StackLen)
			}
			select {
			case out <- at:
			default:
				dropped.Add(1)
				mChanDrops.Inc()
			}
		}
	}()

	sensorName, _ := os.Hostname()
	if n := os.Getenv("NODE_NAME"); n != "" {
		sensorName = n
	}

	if *stdout {
		for at := range out {
			fmt.Printf("%s | %s | %s@%s | %s\n",
				model.NsToTime(at.Timestamp).Format(time.RFC3339Nano), at.Service, at.Package, at.Version, at.Behavior)
		}
		return
	}

	client, err := newCollectorClient(*tlsCA)
	if err != nil {
		log.Fatalf("collector TLS: %v", err)
	}
	go reportCoverageLoop(ctx, client, *collectorURL, *ingestToken, sensorName)
	sendBatches(ctx, client, *collectorURL, *ingestToken, sensorName, out, *batchEvery, *spoolEvents, &dropped)
}

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

func sendBatches(ctx context.Context, client *http.Client, baseURL, token, sensor string, in <-chan model.Attributed, every time.Duration, spoolCap int, dropped *atomic.Uint64) {
	var buf []model.Attributed
	sp := spool.New(spoolCap)
	t := time.NewTicker(every)
	defer t.Stop()

	post := func(events []model.Attributed) error {
		batch := model.EventBatch{Sensor: sensor, Events: events}
		var body bytes.Buffer
		gz := gzip.NewWriter(&body)
		if err := json.NewEncoder(gz).Encode(batch); err != nil {
			return err
		}
		gz.Close()
		req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/events", &body)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("collector returned %s", resp.Status)
		}
		return nil
	}

	flush := func() {
		// Drain spool first, then the current buffer, as one combined send.
		pending := sp.TakeAll()
		if len(buf) > 0 {
			pending = append(pending, buf...)
			buf = nil
		}
		if len(pending) == 0 {
			// Heartbeat so quiet sensors stay visible on the Coverage panel.
			if err := post(nil); err != nil {
				mBatches.WithLabelValues("error").Inc()
			} else {
				mBatches.WithLabelValues("ok").Inc()
			}
			mSpoolDepth.Set(float64(sp.Len()))
			return
		}
		if err := post(pending); err != nil {
			mBatches.WithLabelValues("error").Inc()
			log.Printf("send batch (%d events): %v — spooling", len(pending), err)
			if n := sp.Push(pending); n > 0 {
				mSpoolDropped.Add(float64(n))
				log.Printf("spool full: evicted %d oldest events", n)
			}
		} else {
			mBatches.WithLabelValues("ok").Inc()
		}
		mSpoolDepth.Set(float64(sp.Len()))
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			if d := dropped.Load(); d > 0 {
				log.Printf("shutdown: %d events were dropped on the send buffer", d)
			}
			return
		case at, ok := <-in:
			if !ok {
				flush()
				return
			}
			buf = append(buf, at)
			if len(buf) >= 5000 {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envIntOr(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDurOr(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// reportCoverageLoop posts namespace injection coverage to the collector when
// running in-cluster. Outside a cluster it is a no-op (ScanClusterCoverage fails).
func reportCoverageLoop(ctx context.Context, client *http.Client, baseURL, token, sensor string) {
	interval := envDurOr("GOODMAN_COVERAGE_INTERVAL", 5*time.Minute)
	t := time.NewTicker(interval)
	defer t.Stop()
	report := func() {
		rows, err := coverage.ScanClusterCoverage(sensor)
		if err != nil {
			return // not in cluster, or transient API error
		}
		body, err := json.Marshal(map[string]any{"sensor": sensor, "namespaces": rows})
		if err != nil {
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/coverage", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("coverage report: %v", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Printf("coverage report: collector returned %s", resp.Status)
		}
	}
	report()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			report()
		}
	}
}
