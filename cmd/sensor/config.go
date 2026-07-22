// Sensor flag and environment parsing helpers.
package main

import (
	"flag"
	"os"
	"strconv"
	"strings"
	"time"
)

type sensorConfig struct {
	CollectorURL   string
	ProcRoot       string
	Stdout         bool
	RawStdout      bool
	BatchEvery     time.Duration
	MetricsAddr    string
	ExtraComms     string
	WatchInterval  time.Duration
	IngestToken    string
	TLSCA          string
	ConnectCIDR    int
	SpoolEvents    int
	EnforceEnabled bool
	CgroupRoot     string
	EnforceCgroups multiString
}

func parseSensorConfig() sensorConfig {
	var config sensorConfig
	flag.StringVar(&config.CollectorURL, "collector", envOr("GOODMAN_COLLECTOR_URL", "http://127.0.0.1:8844"), "collector base URL")
	flag.StringVar(&config.ProcRoot, "proc-root", envOr("GOODMAN_PROC_ROOT", "/proc"), "proc mount of the host ('/host/proc' in k8s)")
	flag.BoolVar(&config.Stdout, "stdout", false, "print attributed events to stdout instead of sending to the collector")
	flag.BoolVar(&config.RawStdout, "raw", false, "with -stdout: also print raw events incl. stack addresses")
	flag.DurationVar(&config.BatchEvery, "batch-interval", envDurOr("GOODMAN_BATCH_INTERVAL", 1500*time.Millisecond), "collector flush interval")
	flag.StringVar(&config.MetricsAddr, "metrics-addr", envOr("GOODMAN_METRICS_ADDR", ":9478"), "Prometheus metrics listen address ('' to disable)")
	flag.StringVar(&config.ExtraComms, "comms", os.Getenv("GOODMAN_EXTRA_COMMS"), "extra comm names to watch, comma-separated")
	flag.DurationVar(&config.WatchInterval, "watch-interval", 3*time.Second, "how often to rescan /proc for runtime processes")
	flag.StringVar(&config.IngestToken, "ingest-token", os.Getenv("GOODMAN_INGEST_TOKEN"), "bearer token sent with event batches")
	flag.StringVar(&config.TLSCA, "tls-ca", os.Getenv("GOODMAN_TLS_CA"), "PEM CA bundle to trust for an https collector (empty = system roots)")
	flag.IntVar(&config.ConnectCIDR, "connect-cidr", envIntOr("GOODMAN_CONNECT_CIDR", 0), "aggregate public destination IPs to this IPv4 prefix in CONNECT behaviors (8-32; 0 = exact IPs)")
	flag.IntVar(&config.SpoolEvents, "spool-events", envIntOr("GOODMAN_SPOOL_EVENTS", 50_000), "max attributed events to retain when the collector is unreachable")
	flag.BoolVar(&config.EnforceEnabled, "enforce-enabled", envBoolOr("GOODMAN_ENFORCE_ENABLED", false), "load LSM enforcement programs (default false)")
	flag.StringVar(&config.CgroupRoot, "cgroup-root", envOr("GOODMAN_CGROUP_ROOT", "/sys/fs/cgroup"), "host cgroup2 mount for enforcement scope")
	flag.Var(&config.EnforceCgroups, "enforce-cgroup", "SERVICE=cgroup2-path subject to enforcement (repeatable; e2e/lab)")
	flag.Parse()
	return config
}

// multiString is a repeatable string flag.
type multiString []string

func (m *multiString) String() string { return strings.Join(*m, ",") }

func (m *multiString) Set(v string) error {
	*m = append(*m, v)
	return nil
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

func envBoolOr(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "TRUE"
}
