// Portable product demo command.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hi-heisenbug/goodman/internal/demo"
)

// cmdDemo starts a local collector with seeded alerts, an OpenClaw skill drift,
// a reachability report (1,400 / 240), and a live Mini-Shai-Hulud replay.
func cmdDemo(args []string) {
	fs := flag.NewFlagSet("demo", flag.ExitOnError)
	host := fs.String("host", envOr("GOODMAN_DEMO_HOST", "127.0.0.1"), "listen host")
	port := fs.String("port", envOr("GOODMAN_DEMO_PORT", "8844"), "listen port")
	db := fs.String("db", envOr("GOODMAN_DEMO_DB", "demo_build/goodman_demo.db"), "sqlite path")
	bin := fs.String("collector-bin", envOr("GOODMAN_COLLECTOR_BIN", "bin/collector"), "path to collector binary")
	delay := fs.Duration("attack-delay", 12*time.Second, "wait before replaying the Mini-Shai-Hulud attack")
	check := fs.Bool("check", false, "seed, verify reachability + attack, then exit (CI)")
	fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opt := demo.Options{
		Host:         *host,
		Port:         *port,
		DB:           *db,
		CollectorBin: *bin,
		AttackDelay:  *delay,
		Check:        *check,
	}
	if err := demo.Run(ctx, opt); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
