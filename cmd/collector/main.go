// The Goodman collector composes storage, policy engines, APIs, and background services.
package main

import (
	"context"
	"log"
	"os/signal"
)

func main() {
	config := parseCollectorConfig()
	log.SetPrefix("collector: ")

	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()

	if err := runCollector(ctx, config); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
