// Sensor Prometheus metrics.
package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
	mReadDiscards = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "goodman_sensor_ringbuf_discards_total",
		Help: "Ring-buffer records discarded in userspace because they were malformed or undersized."})
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
	mDenied = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "goodman_sensor_denied_total",
		Help: "Kernel LSM deny events attributed and shipped."}, []string{"type"})
)
