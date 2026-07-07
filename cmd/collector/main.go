// The Goodman collector: receives attributed events from sensors, maintains
// fingerprints, runs the diff engine, serves the alerts API and the
// dashboard.
package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/goodman-sec/goodman/internal/api"
	"github.com/goodman-sec/goodman/internal/api/ui"
	"github.com/goodman-sec/goodman/internal/diff"
	"github.com/goodman-sec/goodman/internal/fingerprint"
	"github.com/goodman-sec/goodman/internal/store"
)

func main() {
	var (
		listen    = flag.String("listen", envOr("GOODMAN_LISTEN", ":8844"), "listen address")
		dsn       = flag.String("dsn", envOr("GOODMAN_DSN", "goodman.db"), "postgres://... or sqlite path")
		learnObs  = flag.Int("learn-obs", envIntOr("GOODMAN_LEARN_OBS", 500), "observations required before baseline promotion")
		learnAge  = flag.Duration("learn-min-age", envDurOr("GOODMAN_LEARN_MIN_AGE", 24*time.Hour), "wall-clock age required before baseline promotion")
		rulesPath = flag.String("rules", os.Getenv("GOODMAN_RULES"), "path to high-risk rules JSON (empty = built-in defaults)")
	)
	flag.Parse()
	log.SetPrefix("collector: ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(*dsn)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	rules, err := diff.LoadRules(*rulesPath)
	if err != nil {
		log.Fatalf("load rules: %v", err)
	}

	fpEng := fingerprint.NewEngine(st, fingerprint.LearningWindow{MinObs: *learnObs, MinAge: *learnAge})
	diffEng := diff.NewEngine(st, rules)
	srv := api.NewServer(st, fpEng, diffEng)

	var uiFS fs.FS
	if sub, err := fs.Sub(ui.Dist, "dist"); err == nil {
		uiFS = sub
	}

	log.Printf("listening on %s (learning window: %d obs / %s; %d rules)",
		*listen, *learnObs, *learnAge, len(rules))
	if err := api.Serve(ctx, *listen, srv.Router(uiFS)); err != nil && ctx.Err() == nil {
		log.Fatal(err)
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
