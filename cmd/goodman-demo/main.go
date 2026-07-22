// goodman-demo is the portable, no-root product walkthrough. It deliberately
// excludes the eBPF loader so the demo can build on developer laptops and in a
// small Docker image while exercising the real collector pipeline.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/hi-heisenbug/goodman/internal/demo"
)

func main() {
	opt := demo.DefaultOptions()
	flag.StringVar(&opt.Host, "host", opt.Host, "collector/dashboard bind host")
	flag.StringVar(&opt.Port, "port", opt.Port, "collector/dashboard port")
	flag.StringVar(&opt.DB, "db", opt.DB, "demo SQLite database path")
	flag.StringVar(&opt.CollectorBin, "collector", opt.CollectorBin, "collector binary path")
	flag.DurationVar(&opt.AttackDelay, "attack-delay", 12*time.Second, "delay before the live replay")
	flag.BoolVar(&opt.Check, "check", false, "verify the complete demo and exit")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := demo.Run(ctx, opt); err != nil {
		log.Fatal(err)
	}
}
