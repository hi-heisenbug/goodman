package fingerprint_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/diff"
	"github.com/hi-heisenbug/goodman/internal/fingerprint"
	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/store"
)

// BenchmarkIngestPipeline measures the collector-side hot path per event:
// fingerprint aggregation + diff evaluation + SQLite persistence. This is the
// throughput a single collector sustains ingesting attributed events; it is
// the number to cite for collector sizing. Sensor-side (eBPF capture +
// attribution) is measured separately on a real kernel; see docs/performance.md.
func BenchmarkIngestPipeline(b *testing.B) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()

	rules, err := diff.LoadRules("")
	if err != nil {
		b.Fatal(err)
	}
	fpEng := fingerprint.NewEngine(st, fingerprint.LearningWindow{MinObs: 500, MinAge: time.Hour})
	diffEng := diff.NewEngine(st, rules)

	// A realistic mix: 50 packages each doing a handful of behaviors, batched
	// the way a sensor flushes them.
	const packages = 50
	batch := make([]model.Attributed, 0, packages*4)
	for p := 0; p < packages; p++ {
		pkg := fmt.Sprintf("pkg-%d", p)
		for _, beh := range []string{
			"READ /app/node_modules/" + pkg + "/**",
			"CONNECT 10.0.0.5:5432",
			"READ /app/config/**",
			"CONNECT 52.84.0.0/16:443",
		} {
			batch = append(batch, model.Attributed{
				Service: "web", Package: pkg, Version: "1.0.0",
				Behavior: beh, Timestamp: 1, Sensor: "node-1",
			})
		}
	}

	b.ResetTimer()
	var events int64
	ts := uint64(1)
	for i := 0; i < b.N; i++ {
		ts++
		for j := range batch {
			batch[j].Timestamp = ts
		}
		ups, err := fpEng.Ingest(ctx, batch)
		if err != nil {
			b.Fatal(err)
		}
		for _, up := range ups {
			if _, err := diffEng.React(ctx, up); err != nil {
				b.Fatal(err)
			}
		}
		events += int64(len(batch))
	}
	b.StopTimer()
	// Report per-event cost and derived throughput.
	nsPerEvent := float64(b.Elapsed().Nanoseconds()) / float64(events)
	b.ReportMetric(nsPerEvent, "ns/event")
	b.ReportMetric(1e9/nsPerEvent, "events/sec")
}
