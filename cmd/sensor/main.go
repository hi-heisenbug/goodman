// The Goodman sensor composes kernel capture, attribution, and collector delivery.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	config := parseSensorConfig()
	log.SetPrefix("sensor: ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runSensor(ctx, config); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
