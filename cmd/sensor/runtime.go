package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hi-heisenbug/goodman/internal/attribute"
	"github.com/hi-heisenbug/goodman/internal/loader"
	"github.com/hi-heisenbug/goodman/internal/model"
)

func runSensor(ctx context.Context, config sensorConfig) error {
	l, err := loader.NewWithOptions(loader.Options{ProcRoot: config.ProcRoot, Enforce: config.EnforceEnabled})
	if err != nil {
		return fmt.Errorf("load eBPF: %w", err)
	}
	defer l.Close()
	log.Printf("eBPF programs attached (open/openat/openat2, connect, execve, fork/exit); proc root %s; enforce=%t active=%t",
		config.ProcRoot, config.EnforceEnabled, l.EnforcementActive())

	startMetricsServer(ctx, config.MetricsAddr)
	resolver := attribute.NewResolver(config.ProcRoot)
	resolver.ConnectCIDRBits = config.ConnectCIDR
	startProcessWatcher(ctx, l, resolver, config.ExtraComms, config.WatchInterval)

	events, dropped := startEventReader(ctx, l, resolver, config.Stdout && config.RawStdout)
	if config.Stdout {
		printAttributedEvents(events)
		return nil
	}

	client, err := newCollectorClient(config.TLSCA)
	if err != nil {
		return fmt.Errorf("collector TLS: %w", err)
	}
	sensor := sensorName()
	go reportCoverageLoop(ctx, client, config.CollectorURL, config.IngestToken, sensor)
	startEnforcementLoops(ctx, config, client, l, sensor)
	sendBatches(ctx, client, config.CollectorURL, config.IngestToken, sensor,
		events, config.BatchEvery, config.SpoolEvents, dropped)
	return nil
}

func startMetricsServer(ctx context.Context, addr string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics server: %v", err)
		}
	}()
}

func startProcessWatcher(ctx context.Context, l *loader.Loader, resolver *attribute.Resolver, extraComms string, interval time.Duration) {
	comms := strings.Split(extraComms, ",")
	refresh := func() { refreshWatched(l, resolver, comms) }
	refresh()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
}

func refreshWatched(l *loader.Loader, resolver *attribute.Resolver, comms []string) {
	err := l.RefreshWatched(comms,
		func(pid uint32) { log.Printf("watching pid %d", pid) },
		func(pid uint32) {
			resolver.Forget(int(pid))
			log.Printf("pid %d exited", pid)
		})
	if err != nil {
		log.Printf("refresh watched pids: %v", err)
	}
	mWatched.Set(float64(len(l.Watched())))
	if drops, err := l.Drops(); err != nil {
		log.Printf("read kernel drop counter: %v", err)
	} else {
		mKernelDrops.Set(float64(drops))
	}
	mReadDiscards.Set(float64(l.Discards()))
}

func startEventReader(ctx context.Context, l *loader.Loader, resolver *attribute.Resolver, rawStdout bool) (<-chan model.Attributed, *atomic.Uint64) {
	out := make(chan model.Attributed, 8192)
	dropped := new(atomic.Uint64)
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	go readEvents(ctx, l, resolver, loader.BootToUnixNs(), rawStdout, out, dropped)
	return out, dropped
}

func readEvents(ctx context.Context, l *loader.Loader, resolver *attribute.Resolver, bootOffset uint64, rawStdout bool, out chan<- model.Attributed, dropped *atomic.Uint64) {
	defer close(out)
	for {
		event, err := l.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) || ctx.Err() != nil {
				return
			}
			log.Printf("ringbuf read: %v", err)
			continue
		}
		mEvents.WithLabelValues(model.EventType(event.Type).String()).Inc()
		attributed := resolver.Attribute(event, bootOffset)
		recordAttribution(event, &attributed)
		if rawStdout {
			fmt.Printf("RAW pid=%d comm=%s type=%s arg=%q stack=%#x\n",
				event.PID, event.CommString(), model.EventType(event.Type), event.ArgString(), event.UserStack())
		}
		select {
		case out <- attributed:
		default:
			dropped.Add(1)
			mChanDrops.Inc()
		}
	}
}

func recordAttribution(event *model.RawEvent, attributed *model.Attributed) {
	if isDenyEvent(event.Type) {
		attributed.Denied = true
		mDenied.WithLabelValues(model.EventType(event.Type).String()).Inc()
	}
	switch attributed.Package {
	case "<unknown>":
		mAttributed.WithLabelValues("unknown").Inc()
	case "<app>":
		mAttributed.WithLabelValues("app").Inc()
	default:
		mAttributed.WithLabelValues("package").Inc()
	}
}

func sensorName() string {
	if node := os.Getenv("NODE_NAME"); node != "" {
		return node
	}
	host, _ := os.Hostname()
	return host
}

func printAttributedEvents(events <-chan model.Attributed) {
	for attributed := range events {
		fmt.Printf("%s | %s | %s@%s | %s\n",
			model.NsToTime(attributed.Timestamp).Format(time.RFC3339Nano),
			attributed.Service, attributed.Package, attributed.Version, attributed.Behavior)
	}
}

func startEnforcementLoops(ctx context.Context, config sensorConfig, client *http.Client, l *loader.Loader, sensor string) {
	if !config.EnforceEnabled {
		return
	}
	reconciler := newEnforcementReconciler(l)
	go enforceScopeLoop(ctx, reconciler, config.CgroupRoot, sensor, []string(config.EnforceCgroups))
	go enforcePollLoop(ctx, client, l, reconciler, config.CollectorURL, config.IngestToken, sensor)
}
