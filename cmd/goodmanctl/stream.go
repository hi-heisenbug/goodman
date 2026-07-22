// Live SSE streaming and reconnect behavior.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func cmdTail(args []string) {
	fs := flag.NewFlagSet("tail", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	delay := time.Second
	for {
		err := tailStream(ctx, *collector, *token)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errUnauthorized) {
			log.Fatal("collector returned 401 unauthorized: set GOODMAN_API_TOKEN or pass -token")
		}
		log.Printf("stream disconnected (%v); reconnecting in %s", err, delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

func tailStream(ctx context.Context, collector, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, collector+"/v1/stream", nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("collector returned %s", resp.Status)
	}
	log.Printf("connected to %s — streaming (Ctrl-C to stop)", collector)

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	printer := ssePrinter{}
	for sc.Scan() {
		printer.consume(sc.Text())
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF
}

type ssePrinter struct {
	event string
}

func (p *ssePrinter) consume(line string) {
	if value, ok := strings.CutPrefix(line, "event: "); ok {
		p.event = value
		return
	}
	if value, ok := strings.CutPrefix(line, "data: "); ok {
		printSSEData(p.event, value)
	}
}

func printSSEData(event, data string) {
	switch event {
	case "events":
		var events []model.Attributed
		if json.Unmarshal([]byte(data), &events) == nil {
			printStreamEvents(events)
		}
	case "alerts":
		var alerts []model.Alert
		if json.Unmarshal([]byte(data), &alerts) == nil {
			printStreamAlerts(alerts)
		}
	}
}

func printStreamEvents(events []model.Attributed) {
	for _, event := range events {
		fmt.Printf("%s | %-20s | %s@%s | %s\n",
			model.NsToTime(event.Timestamp).Format("15:04:05.000"),
			event.Service, event.Package, orDash(event.Version), event.Behavior)
	}
}

func printStreamAlerts(alerts []model.Alert) {
	for _, alert := range alerts {
		fmt.Printf("\n🚨 [%s] %s: %s %s → %s\n", alert.Severity, alert.Service, alert.Package,
			orDash(alert.OldVersion), alert.NewVersion)
		for _, behavior := range alert.NewBehaviors {
			fmt.Printf("     + %s\n", behavior)
		}
		if len(alert.MatchedRules) > 0 {
			fmt.Printf("     rules: %s\n", strings.Join(alert.MatchedRules, ", "))
		}
		fmt.Println()
	}
}
