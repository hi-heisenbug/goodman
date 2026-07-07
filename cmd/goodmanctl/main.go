// goodmanctl is the dev/ops CLI: tail the live event stream, list alerts
// and fingerprints, and run one-shot attribution against a live pid.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/goodman-sec/goodman/internal/attribute"
	"github.com/goodman-sec/goodman/internal/loader"
	"github.com/goodman-sec/goodman/internal/model"
)

const usage = `goodmanctl — Goodman dev CLI

Usage:
  goodmanctl tail          [-collector URL]                stream live events + alerts
  goodmanctl alerts        [-collector URL] [-status S]    list alerts
  goodmanctl ack ID        [-collector URL]                acknowledge an alert
  goodmanctl resolve ID    [-collector URL]                resolve an alert
  goodmanctl fingerprints  [-collector URL] [-service S] [-package P]
  goodmanctl attribute -pid N [-duration 15s] [-proc-root /proc]
                                                            live-attribute one pid (needs root)
`

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "tail":
		cmdTail(args)
	case "alerts":
		cmdAlerts(args)
	case "ack":
		cmdAck(args)
	case "resolve":
		cmdResolve(args)
	case "fingerprints":
		cmdFingerprints(args)
	case "attribute":
		cmdAttribute(args)
	default:
		fmt.Print(usage)
		os.Exit(2)
	}
}

func collectorFlag(fs *flag.FlagSet) *string {
	def := os.Getenv("GOODMAN_COLLECTOR_URL")
	if def == "" {
		def = "http://127.0.0.1:8844"
	}
	return fs.String("collector", def, "collector base URL")
}

func cmdTail(args []string) {
	fs := flag.NewFlagSet("tail", flag.ExitOnError)
	collector := collectorFlag(fs)
	fs.Parse(args)

	resp, err := http.Get(*collector + "/v1/stream")
	if err != nil {
		log.Fatalf("connect %s: %v", *collector, err)
	}
	defer resp.Body.Close()
	log.Printf("connected to %s — streaming (Ctrl-C to stop)", *collector)

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			switch event {
			case "events":
				var evs []model.Attributed
				if json.Unmarshal([]byte(data), &evs) == nil {
					for _, e := range evs {
						fmt.Printf("%s | %-20s | %s@%s | %s\n",
							model.NsToTime(e.Timestamp).Format("15:04:05.000"),
							e.Service, e.Package, orDash(e.Version), e.Behavior)
					}
				}
			case "alerts":
				var alerts []model.Alert
				if json.Unmarshal([]byte(data), &alerts) == nil {
					for _, a := range alerts {
						fmt.Printf("\n🚨 [%s] %s: %s %s → %s\n", a.Severity, a.Service, a.Package,
							orDash(a.OldVersion), a.NewVersion)
						for _, b := range a.NewBehaviors {
							fmt.Printf("     + %s\n", b)
						}
						fmt.Println()
					}
				}
			}
		}
	}
}

func cmdAlerts(args []string) {
	fs := flag.NewFlagSet("alerts", flag.ExitOnError)
	collector := collectorFlag(fs)
	status := fs.String("status", "", "filter by status (open|acknowledged|resolved)")
	asJSON := fs.Bool("json", false, "raw JSON output")
	fs.Parse(args)

	u := *collector + "/v1/alerts"
	if *status != "" {
		u += "?status=" + url.QueryEscape(*status)
	}
	var alerts []model.Alert
	getJSON(u, &alerts)
	if *asJSON {
		json.NewEncoder(os.Stdout).Encode(alerts)
		return
	}
	if len(alerts) == 0 {
		fmt.Println("no alerts")
		return
	}
	for _, a := range alerts {
		fmt.Printf("%s  [%-8s] %-12s %s: %s %s → %s\n",
			model.NsToTime(a.DetectedAt).Format("2006-01-02 15:04:05"),
			a.Severity, a.Status, a.Service, a.Package, orDash(a.OldVersion), a.NewVersion)
		for _, b := range a.NewBehaviors {
			fmt.Printf("    + %s\n", b)
		}
		fmt.Printf("    id: %s\n", a.ID)
	}
}

func cmdAck(args []string) {
	postAlertAction(args, "ack", "acknowledged")
}

func cmdResolve(args []string) {
	postAlertAction(args, "resolve", "resolved")
}

func postAlertAction(args []string, action, done string) {
	fs := flag.NewFlagSet("ack", flag.ExitOnError)
	collector := collectorFlag(fs)
	fs.Parse(args)
	if fs.NArg() != 1 {
		log.Fatalf("usage: goodmanctl %s <alert-id>", action)
	}
	resp, err := http.Post(*collector+"/v1/alerts/"+url.PathEscape(fs.Arg(0))+"/"+action, "application/json", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("collector returned %s", resp.Status)
	}
	fmt.Println(done)
}

func cmdFingerprints(args []string) {
	fs := flag.NewFlagSet("fingerprints", flag.ExitOnError)
	collector := collectorFlag(fs)
	service := fs.String("service", "", "filter by service")
	pkg := fs.String("package", "", "filter by package")
	asJSON := fs.Bool("json", false, "raw JSON output")
	fs.Parse(args)

	u := *collector + "/v1/fingerprints?service=" + url.QueryEscape(*service) + "&package=" + url.QueryEscape(*pkg)
	var fps []model.Fingerprint
	getJSON(u, &fps)
	if *asJSON {
		json.NewEncoder(os.Stdout).Encode(fps)
		return
	}
	for _, fp := range fps {
		state := "learning"
		if fp.IsBaseline {
			state = "BASELINE"
		}
		fmt.Printf("%s / %s@%s  [%s]  obs=%d  behaviors=%d\n",
			fp.Service, fp.Package, orDash(fp.Version), state, fp.ObsCount, len(fp.Behaviors))
		keys := make([]string, 0, len(fp.Behaviors))
		for b := range fp.Behaviors {
			keys = append(keys, b)
		}
		sort.Strings(keys)
		for _, b := range keys {
			st := fp.Behaviors[b]
			fmt.Printf("    %-60s x%d\n", b, st.Count)
		}
	}
}

// cmdAttribute loads the eBPF sensor for a single pid and prints attributed
// events live — the §5 DoD check. Needs root.
func cmdAttribute(args []string) {
	fs := flag.NewFlagSet("attribute", flag.ExitOnError)
	pid := fs.Int("pid", 0, "target pid (required)")
	dur := fs.Duration("duration", 15*time.Second, "how long to trace")
	procRoot := fs.String("proc-root", "/proc", "proc mount")
	showStacks := fs.Bool("stacks", false, "print resolved stack frames per event")
	fs.Parse(args)
	if *pid <= 0 {
		log.Fatal("usage: goodmanctl attribute -pid <pid> [-duration 15s] [-stacks]")
	}

	l, err := loader.New(*procRoot)
	if err != nil {
		log.Fatalf("load eBPF (need root): %v", err)
	}
	defer l.Close()
	if err := l.Watch(uint32(*pid)); err != nil {
		log.Fatalf("watch pid %d: %v", *pid, err)
	}
	resolver := attribute.NewResolver(*procRoot)
	bootOffset := loader.BootToUnixNs()
	log.Printf("tracing pid %d for %s…", *pid, *dur)

	go func() {
		time.Sleep(*dur)
		l.Close()
	}()
	count := map[string]int{}
	for {
		ev, err := l.Read()
		if err != nil {
			break
		}
		at := resolver.Attribute(ev, bootOffset)
		fmt.Printf("%s | %s@%s | %s\n", at.Service, at.Package, orDash(at.Version), at.Behavior)
		count[at.Package]++
		if *showStacks {
			for _, f := range resolver.ResolveStack(*pid, ev.UserStack()) {
				loc := f.Symbol
				if f.Source != "" {
					loc = f.Source
				}
				fmt.Printf("      %#014x  %s\n", f.Addr, loc)
			}
		}
	}
	fmt.Println("\nattribution summary:")
	for pkg, n := range count {
		fmt.Printf("  %-40s %d\n", pkg, n)
	}
}

func getJSON(u string, v any) {
	resp, err := http.Get(u)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("collector returned %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		log.Fatal(err)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
