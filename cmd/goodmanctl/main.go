// goodmanctl is the dev/ops CLI: tail the live event stream, list alerts
// and fingerprints, and run one-shot attribution against a live pid.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hi-heisenbug/goodman/internal/attribute"
	"github.com/hi-heisenbug/goodman/internal/demo"
	"github.com/hi-heisenbug/goodman/internal/loader"
	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/report"
)

const usage = `goodmanctl — Goodman dev CLI

Usage:
  goodmanctl tail          [-collector URL]                stream live events + alerts
  goodmanctl alerts        [-collector URL] [-status S]    list alerts
  goodmanctl ack ID        [-collector URL]                acknowledge an alert
  goodmanctl resolve ID    [-collector URL]                resolve an alert
  goodmanctl fingerprints  [-collector URL] [-service S] [-package P]
  goodmanctl fingerprints export [-collector URL] [-token T]
  goodmanctl fingerprints import <file> [-collector URL] [-token T]
  goodmanctl report -lockfile package-lock.json [-service S] [-osv] [-o FILE]
                                                            runtime reachability report
  goodmanctl demo          [-host 127.0.0.1] [-port 8844] [-attack-delay 12s]
                                                            five-minute product wow (no root)
  goodmanctl enforce status|on|off [-collector URL] [-token T]
                                                            kernel enforcement runtime switch
  goodmanctl attribute -pid N [-duration 15s] [-proc-root /proc]
                                                            live-attribute one pid (needs root)

All collector commands accept -token (or GOODMAN_API_TOKEN) when the
collector's API requires authentication.
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
		if len(args) > 0 {
			switch args[0] {
			case "export":
				cmdFingerprintsExport(args[1:])
				return
			case "import":
				cmdFingerprintsImport(args[1:])
				return
			}
		}
		cmdFingerprints(args)
	case "report":
		cmdReport(args)
	case "demo":
		cmdDemo(args)
	case "enforce":
		cmdEnforce(args)
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

func tokenFlag(fs *flag.FlagSet) *string {
	return fs.String("token", os.Getenv("GOODMAN_API_TOKEN"), "API bearer token (when the collector requires one)")
}

// doRequest issues an HTTP request with the API token attached and fails the
// command with a hint when the collector rejects the credentials.
func doRequest(method, u, token string) *http.Response {
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		log.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		log.Fatal("collector returned 401 unauthorized: set GOODMAN_API_TOKEN or pass -token")
	}
	return resp
}

func cmdTail(args []string) {
	fs := flag.NewFlagSet("tail", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	fs.Parse(args)

	resp := doRequest(http.MethodGet, *collector+"/v1/stream", *token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("collector returned %s", resp.Status)
	}
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
						if len(a.MatchedRules) > 0 {
							fmt.Printf("     rules: %s\n", strings.Join(a.MatchedRules, ", "))
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
	token := tokenFlag(fs)
	status := fs.String("status", "", "filter by status (open|acknowledged|resolved)")
	asJSON := fs.Bool("json", false, "raw JSON output")
	fs.Parse(args)

	u := *collector + "/v1/alerts"
	if *status != "" {
		u += "?status=" + url.QueryEscape(*status)
	}
	var alerts []model.Alert
	getJSON(u, *token, &alerts)
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
		if len(a.MatchedRules) > 0 {
			fmt.Printf("    rules: %s\n", strings.Join(a.MatchedRules, ", "))
		}
		for _, ev := range a.Evidence {
			if ev.Sensor != "" {
				fmt.Printf("    seen: %s on %s at %s\n", ev.Behavior, ev.Sensor,
					model.NsToTime(ev.FirstSeen).Format("2006-01-02 15:04:05"))
			}
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
	token := tokenFlag(fs)
	fs.Parse(args)
	if fs.NArg() != 1 {
		log.Fatalf("usage: goodmanctl %s <alert-id>", action)
	}
	resp := doRequest(http.MethodPost, *collector+"/v1/alerts/"+url.PathEscape(fs.Arg(0))+"/"+action, *token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("collector returned %s", resp.Status)
	}
	fmt.Println(done)
}

func cmdFingerprints(args []string) {
	fs := flag.NewFlagSet("fingerprints", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	service := fs.String("service", "", "filter by service")
	pkg := fs.String("package", "", "filter by package")
	asJSON := fs.Bool("json", false, "raw JSON output")
	fs.Parse(args)

	u := *collector + "/v1/fingerprints?service=" + url.QueryEscape(*service) + "&package=" + url.QueryEscape(*pkg)
	var fps []model.Fingerprint
	getJSON(u, *token, &fps)
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

func cmdFingerprintsExport(args []string) {
	fs := flag.NewFlagSet("fingerprints export", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	fs.Parse(args)

	resp := doRequest(http.MethodGet, *collector+"/v1/fingerprints/export", *token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("collector returned %s", resp.Status)
	}
	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		log.Fatal(err)
	}
}

func cmdFingerprintsImport(args []string) {
	fs := flag.NewFlagSet("fingerprints import", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	fs.Parse(args)
	if fs.NArg() != 1 {
		log.Fatal("usage: goodmanctl fingerprints import <file>")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		log.Fatalf("read file: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, *collector+"/v1/fingerprints/import", strings.NewReader(string(data)))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if *token != "" {
		req.Header.Set("Authorization", "Bearer "+*token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		log.Fatal("collector returned 401 unauthorized: set GOODMAN_API_TOKEN or pass -token")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("collector returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		Imported           int `json:"imported"`
		SkippedLocal       int `json:"skipped_local"`
		Replaced           int `json:"replaced"`
		IgnoredNonBaseline int `json:"ignored_non_baseline"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("imported=%d skipped_local=%d replaced=%d ignored_non_baseline=%d\n",
		result.Imported, result.SkippedLocal, result.Replaced, result.IgnoredNonBaseline)
}

// cmdReport builds the runtime reachability report: declared dependencies
// (from a lockfile) joined against packages Goodman observed executing, with
// optional OSV vulnerability enrichment.
func cmdReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	lockfile := fs.String("lockfile", "package-lock.json", "path to package-lock.json")
	service := fs.String("service", "", "limit to one service (pod/deployment)")
	useOSV := fs.Bool("osv", false, "enrich with OSV.dev vulnerability data (needs network)")
	out := fs.String("o", "", "write the markdown report to this file (default stdout)")
	fs.Parse(args)

	data, err := os.ReadFile(*lockfile)
	if err != nil {
		log.Fatalf("read lockfile: %v", err)
	}
	declared, err := report.ParseLockfile(data)
	if err != nil {
		log.Fatal(err)
	}
	if len(declared) == 0 {
		log.Fatalf("no packages found in %s", *lockfile)
	}

	u := *collector + "/v1/fingerprints"
	if *service != "" {
		u += "?service=" + url.QueryEscape(*service)
	}
	var fps []model.Fingerprint
	getJSON(u, *token, &fps)

	var vulns map[string][]report.Vulnerability
	if *useOSV {
		log.Printf("querying OSV.dev for %d packages…", len(declared))
		vulns, err = report.NewOSVClient().Query(context.Background(), declared)
		if err != nil {
			log.Fatalf("osv: %v", err)
		}
	}

	rep := report.Build(*service, declared, fps, vulns)
	md := rep.Markdown()
	if *out == "" {
		fmt.Print(md)
	} else if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
		log.Fatalf("write report: %v", err)
	} else {
		log.Printf("wrote %s (%d declared, %d executed)", *out, rep.DeclaredCount, rep.ExecutedCount)
	}
}

// cmdDemo starts a local collector with seeded alerts, a preloaded
// reachability report (1,400 / 240), and a live event-stream attack replay.
func cmdDemo(args []string) {
	fs := flag.NewFlagSet("demo", flag.ExitOnError)
	host := fs.String("host", envOr("GOODMAN_DEMO_HOST", "127.0.0.1"), "listen host")
	port := fs.String("port", envOr("GOODMAN_DEMO_PORT", "8844"), "listen port")
	db := fs.String("db", envOr("GOODMAN_DEMO_DB", "demo_build/goodman_demo.db"), "sqlite path")
	bin := fs.String("collector-bin", envOr("GOODMAN_COLLECTOR_BIN", "bin/collector"), "path to collector binary")
	delay := fs.Duration("attack-delay", 12*time.Second, "wait before replaying the event-stream attack")
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

func getJSON(u, token string, v any) {
	resp := doRequest(http.MethodGet, u, token)
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

func cmdEnforce(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: goodmanctl enforce status|on|off")
	}
	action := args[0]
	fs := flag.NewFlagSet("enforce", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	fs.Parse(args[1:])
	var method, path string
	switch action {
	case "status":
		method, path = http.MethodGet, "/v1/enforce"
	case "on":
		method, path = http.MethodPost, "/v1/enforce/on"
	case "off":
		method, path = http.MethodPost, "/v1/enforce/off"
	default:
		log.Fatal("usage: goodmanctl enforce status|on|off")
	}
	resp := doRequest(method, *collector+path, *token)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("collector returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	fmt.Println(string(body))
}
