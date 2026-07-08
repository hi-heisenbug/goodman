// The Goodman sensor: loads the eBPF programs, attributes each captured
// syscall to an npm package via the user-space stack, and ships batches to
// the collector. Runs as a privileged DaemonSet in k8s or as root locally.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hi-heisenbug/goodman/internal/attribute"
	"github.com/hi-heisenbug/goodman/internal/loader"
	"github.com/hi-heisenbug/goodman/internal/model"
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

	sendBatches(ctx, *collectorURL, sensorName, out, *batchEvery, &dropped)
}

func sendBatches(ctx context.Context, baseURL, sensor string, in <-chan model.Attributed, every time.Duration, dropped *atomic.Uint64) {
	client := &http.Client{Timeout: 10 * time.Second}
	var buf []model.Attributed
	t := time.NewTicker(every)
	defer t.Stop()

	flush := func() {
		if len(buf) == 0 {
			return
		}
		batch := model.EventBatch{Sensor: sensor, Events: buf}
		buf = nil
		var body bytes.Buffer
		gz := gzip.NewWriter(&body)
		if err := json.NewEncoder(gz).Encode(batch); err != nil {
			log.Printf("encode batch: %v", err)
			return
		}
		gz.Close()
		req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/events", &body)
		if err != nil {
			log.Printf("build request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")
		resp, err := client.Do(req)
		if err != nil {
			mBatches.WithLabelValues("error").Inc()
			log.Printf("send batch (%d events): %v", len(batch.Events), err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			mBatches.WithLabelValues("error").Inc()
			log.Printf("collector returned %s", resp.Status)
			return
		}
		mBatches.WithLabelValues("ok").Inc()
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

func envDurOr(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
