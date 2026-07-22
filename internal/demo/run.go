package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
)

// Options controls the interactive (or -check) demo run.
type Options struct {
	Host         string
	Port         string
	DB           string
	CollectorBin string
	AttackDelay  time.Duration
	Check        bool // seed, verify, fire attack, verify, exit
	Stdout       *os.File
	Stderr       *os.File
}

// DefaultOptions returns the local product-demo defaults.
func DefaultOptions() Options {
	return Options{
		Host:         "127.0.0.1",
		Port:         "8844",
		DB:           "demo_build/goodman_demo.db",
		CollectorBin: "bin/collector",
		AttackDelay:  12 * time.Second,
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
	}
}

// URL returns the dashboard base URL.
func (o Options) URL() string {
	host := strings.TrimSpace(o.Host)
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, o.Port)
}

// GuidedScript is the 60-second talk track printed after the collector is ready.
func GuidedScript(url string, attackDelay time.Duration) string {
	return fmt.Sprintf(`Goodman demo is ready.

Dashboard:      %s
Alerts:         %s/#alerts
Reachability:   %s/#reachability
Coverage:       %s/#coverage

── 60-second guided script ──────────────────────────────────
 0–10s  Open the Alerts tab. You already have CRITICAL drifts
        from compromised package versions. Find service=openclaw:
        @goodman-demo/calendar-sync@1.2.3 read OpenClaw creds
        and contacted a new endpoint. The package is fictional.

10–25s  Switch to Reachability. Headline: 1,400 declared
        dependencies, 240 actually executed at runtime — the
        "shipped vs run" prioritization pitch.

25–%ds  Stay on Alerts. In ~%s the 2026 Mini-Shai-Hulud
        behavior profile replays live: .npmrc + cloud metadata +
        C2 + a shell exec. Watch one package-attributed CRITICAL
        row appear with all four rule chips.

%d+     Open Coverage: staging shows as an injection gap
        (unlabeled, pods without NODE_OPTIONS). Attribution
        success and alert-budget burn rate are on the KPI strip.
─────────────────────────────────────────────────────────────
Press Ctrl-C to stop.
`, url, url, url, url, int(attackDelay.Seconds())+25, attackDelay, int(attackDelay.Seconds())+25)
}

// Run starts a local collector, seeds the wow state, optionally fires the
// live attack after AttackDelay, and blocks until the collector exits
// (interactive) or returns after verification (-check).
func Run(ctx context.Context, opt Options) error {
	opt = normalizeOptions(opt)
	absBin, db, err := prepareDemoFiles(opt)
	if err != nil {
		return err
	}
	osvEndpoint, stopOSV, err := startDemoOSVServer()
	if err != nil {
		return fmt.Errorf("start demo OSV stub: %w", err)
	}
	defer stopOSV()

	collector, err := startDemoCollector(ctx, opt, absBin, db, osvEndpoint)
	if err != nil {
		return err
	}
	defer collector.stop()

	c := NewClient(opt.URL())
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.WaitHealthy(waitCtx); err != nil {
		return err
	}
	if err := seedDemoState(ctx, c, opt.Stdout); err != nil {
		return err
	}
	if opt.Check {
		return runCheck(ctx, c, opt)
	}
	return runInteractiveDemo(ctx, c, opt, collector)
}

func normalizeOptions(opt Options) Options {
	if opt.Stdout == nil {
		opt.Stdout = os.Stdout
	}
	if opt.Stderr == nil {
		opt.Stderr = os.Stderr
	}
	if opt.AttackDelay <= 0 && !opt.Check {
		opt.AttackDelay = 12 * time.Second
	}
	if opt.Check {
		opt.AttackDelay = 0
	}
	return opt
}

func prepareDemoFiles(opt Options) (string, string, error) {
	bin := opt.CollectorBin
	if bin == "" {
		bin = "bin/collector"
	}
	absBin, err := filepath.Abs(bin)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(absBin); err != nil {
		return "", "", fmt.Errorf("collector binary not found at %s (run: make build): %w", absBin, err)
	}

	db := opt.DB
	if db == "" {
		db = "demo_build/goodman_demo.db"
	}
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		return "", "", err
	}
	for _, suffix := range []string{"", "-shm", "-wal"} {
		_ = os.Remove(db + suffix)
	}
	return absBin, db, nil
}

type demoCollector struct {
	cmd    *exec.Cmd
	waitCh chan error
	exited bool
}

func startDemoCollector(ctx context.Context, opt Options, absBin, db, osvEndpoint string) (*demoCollector, error) {
	listen := opt.Host + ":" + opt.Port
	cmd := exec.CommandContext(ctx, absBin, "-listen", listen)
	cmd.Env = append(os.Environ(),
		"GOODMAN_DSN="+db,
		"GOODMAN_LEARN_OBS=3",
		"GOODMAN_LEARN_MIN_AGE=1s",
		"GOODMAN_OSV_ENDPOINT="+osvEndpoint,
	)
	cmd.Stdout = opt.Stderr
	cmd.Stderr = opt.Stderr
	configureCollectorProcess(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start collector: %w", err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	return &demoCollector{cmd: cmd, waitCh: waitCh}, nil
}

func (c *demoCollector) stop() {
	if c.exited || c.cmd.Process == nil {
		return
	}
	terminateCollectorProcess(c.cmd)
	select {
	case <-c.waitCh:
	case <-time.After(5 * time.Second):
		_ = c.cmd.Process.Kill()
		<-c.waitCh
	}
	c.exited = true
}

func seedDemoState(ctx context.Context, c *Client, stdout io.Writer) error {
	fmt.Fprintln(stdout, "Seeding product fingerprints and alerts…")
	if err := SeedProduct(ctx, c); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Seeding Mini-Shai-Hulud baseline (mini-shai-hulud-loader@1.0.0)…")
	if err := SeedMiniShaiHuludBaseline(ctx, c); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Replaying OpenClaw skill drift (@goodman-demo/calendar-sync@1.2.3)…")
	if err := SeedOpenClawSkillBaseline(ctx, c); err != nil {
		return err
	}
	if err := FireOpenClawSkillAttack(ctx, c); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Preloading reachability report (1,400 / 240)…")
	rep, err := SeedReachability(ctx, c)
	if err != nil {
		return err
	}
	if rep.DeclaredCount != DeclaredCount || rep.ExecutedCount != ExecutedCount {
		return fmt.Errorf("reachability seed counts: declared=%d executed=%d (want %d/%d)",
			rep.DeclaredCount, rep.ExecutedCount, DeclaredCount, ExecutedCount)
	}
	fmt.Fprintln(stdout, "Seeding coverage panel (incl. unlabeled staging gap)…")
	if err := SeedCoverage(ctx, c); err != nil {
		return err
	}
	return nil
}

func runInteractiveDemo(ctx context.Context, c *Client, opt Options, collector *demoCollector) error {
	go demoHeartbeatLoop(ctx, c)

	fmt.Fprint(opt.Stdout, GuidedScript(opt.URL(), opt.AttackDelay))

	fmt.Fprintf(opt.Stdout, "\nReplaying Mini-Shai-Hulud behavior in %s…\n", opt.AttackDelay)
	timer := time.NewTimer(opt.AttackDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	fmt.Fprintln(opt.Stdout, "Firing mini-shai-hulud-loader@1.0.1 (.npmrc + IMDS + C2 + exec)…")
	if err := FireMiniShaiHuludAttack(ctx, c); err != nil {
		return err
	}
	fmt.Fprintln(opt.Stdout, "Attack injected. Watch the Alerts tab for the new CRITICAL row.")

	select {
	case <-ctx.Done():
		return nil
	case err := <-collector.waitCh:
		collector.exited = true
		return err
	}
}

func demoHeartbeatLoop(ctx context.Context, c *Client) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.PostHeartbeat(ctx); err != nil && ctx.Err() == nil {
				log.Printf("demo heartbeat: %v", err)
			}
		}
	}
}

func startDemoOSVServer() (string, func(), error) {
	advisories := map[string]string{
		"lodash@4.17.20":     "GHSA-35jh-r3h4-6jhm",
		"jsonwebtoken@8.5.1": "GHSA-8cf7-32gw-wr33",
		"axios@0.21.1":       "GHSA-42xw-2xvc-qx8m",
	}
	details := map[string]map[string]any{
		"GHSA-35jh-r3h4-6jhm": {"summary": "Command injection in lodash template helpers", "database_specific": map[string]string{"severity": "HIGH"}},
		"GHSA-8cf7-32gw-wr33": {"summary": "jsonwebtoken signature validation weakness", "database_specific": map[string]string{"severity": "HIGH"}},
		"GHSA-42xw-2xvc-qx8m": {"summary": "Axios server-side request forgery", "database_specific": map[string]string{"severity": "MODERATE"}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/querybatch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Queries []struct {
				Version string            `json:"version"`
				Package map[string]string `json:"package"`
			} `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		results := make([]map[string]any, len(body.Queries))
		for i, query := range body.Queries {
			vulns := []map[string]string{}
			if id := advisories[query.Package["name"]+"@"+query.Version]; id != "" {
				vulns = append(vulns, map[string]string{"id": id})
			}
			results[i] = map[string]any{"vulns": vulns}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	})
	for id, detail := range details {
		id, detail := id, detail
		mux.HandleFunc("/v1/vulns/"+id, func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(detail)
		})
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = server.Serve(listener)
	}()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	return "http://" + listener.Addr().String() + "/v1/querybatch", stop, nil
}

func runCheck(ctx context.Context, c *Client, opt Options) error {
	if err := verifyDashboard(ctx, c); err != nil {
		return err
	}
	declared, executed, err := verifyReachability(ctx, c)
	if err != nil {
		return err
	}
	if err := verifyCoverage(ctx, c); err != nil {
		return err
	}
	snapshotSchema, exportSchema, openClaw, err := verifyOpenClawState(ctx, c)
	if err != nil {
		return err
	}
	attack, err := fireAndWaitForMiniShaiHulud(ctx, c)
	if err != nil {
		return err
	}
	fmt.Fprintf(opt.Stdout, "demo check OK: dashboard, snapshot %s, export %s, reachability %d/%d, CRITICAL OpenClaw %s@%s and %s@%s→%s\n",
		snapshotSchema, exportSchema, declared, executed, openClaw.Package, openClaw.Malicious.Version,
		attack.Package, attack.OldVersion, attack.NewVersion)
	return nil
}

func verifyReachability(ctx context.Context, c *Client) (int, int, error) {
	stored, err := c.GetReport(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("check stored report: %w", err)
	}
	if stored.DeclaredCount != DeclaredCount || stored.ExecutedCount != ExecutedCount {
		return 0, 0, fmt.Errorf("stored report: declared=%d executed=%d (want %d/%d)",
			stored.DeclaredCount, stored.ExecutedCount, DeclaredCount, ExecutedCount)
	}
	reachableVulns := 0
	for _, row := range stored.VulnRows {
		if row.Executed {
			reachableVulns += len(row.Vulns)
		}
	}
	if reachableVulns < 3 {
		return 0, 0, fmt.Errorf("stored report: reachable vulnerabilities=%d, want at least 3", reachableVulns)
	}
	return stored.DeclaredCount, stored.ExecutedCount, nil
}

func verifyCoverage(ctx context.Context, c *Client) error {
	covReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/coverage", nil)
	if err != nil {
		return err
	}
	covResp, err := c.HTTP.Do(covReq)
	if err != nil {
		return err
	}
	defer covResp.Body.Close()
	if covResp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /v1/coverage: %s", covResp.Status)
	}
	var cov struct {
		Namespaces []struct {
			Name        string `json:"name"`
			InjectLabel bool   `json:"inject_label"`
			PodsWithout int    `json:"pods_without"`
		} `json:"namespaces"`
		Sensors []struct {
			Name string `json:"name"`
		} `json:"sensors"`
	}
	if err := json.NewDecoder(covResp.Body).Decode(&cov); err != nil {
		return err
	}
	gapOK := false
	for _, ns := range cov.Namespaces {
		if ns.Name == "staging" && !ns.InjectLabel && ns.PodsWithout > 0 {
			gapOK = true
			break
		}
	}
	if !gapOK {
		return fmt.Errorf("check: staging injection gap missing from coverage: %+v", cov.Namespaces)
	}
	if len(cov.Sensors) == 0 {
		return fmt.Errorf("check: expected demo sensor in coverage")
	}
	return nil
}

func verifyOpenClawState(ctx context.Context, c *Client) (string, string, Scenario, error) {
	openClaw := OpenClawSkillScenario()
	alerts, err := c.GetAlerts(ctx)
	if err != nil {
		return "", "", Scenario{}, err
	}
	if !hasCriticalScenarioAlert(alerts, openClaw) {
		return "", "", Scenario{}, fmt.Errorf("check: no CRITICAL OpenClaw skill alert for %s@%s (%d alerts total)",
			openClaw.Package, openClaw.Malicious.Version, len(alerts))
	}
	snapshot, err := c.GetSnapshot(ctx)
	if err != nil {
		return "", "", Scenario{}, fmt.Errorf("check snapshot: %w", err)
	}
	if snapshot.Schema != "goodman.snapshot/v1" || snapshot.GeneratedAt == 0 {
		return "", "", Scenario{}, fmt.Errorf("check: bad snapshot envelope: schema=%q generated_at=%d",
			snapshot.Schema, snapshot.GeneratedAt)
	}
	if !hasCriticalScenarioAlert(snapshot.Alerts, openClaw) || !hasScenarioFingerprint(snapshot.Fingerprints, openClaw) {
		return "", "", Scenario{}, fmt.Errorf("check: snapshot missing OpenClaw alert/fingerprint: alerts=%d fingerprints=%d",
			len(snapshot.Alerts), len(snapshot.Fingerprints))
	}
	exported, err := c.GetExport(ctx)
	if err != nil {
		return "", "", Scenario{}, fmt.Errorf("check export: %w", err)
	}
	if exported.Schema != "goodman.export/v1" || exported.GeneratedAt == 0 {
		return "", "", Scenario{}, fmt.Errorf("check: bad export envelope: schema=%q generated_at=%d",
			exported.Schema, exported.GeneratedAt)
	}
	if !hasCriticalScenarioAlert(exported.Alerts, openClaw) || !hasScenarioFingerprint(exported.Fingerprints, openClaw) ||
		len(exported.Reachability) == 0 {
		return "", "", Scenario{}, fmt.Errorf("check: export missing OpenClaw state: alerts=%d fingerprints=%d reachability=%d",
			len(exported.Alerts), len(exported.Fingerprints), len(exported.Reachability))
	}
	return snapshot.Schema, exported.Schema, openClaw, nil
}

func fireAndWaitForMiniShaiHulud(ctx context.Context, c *Client) (*model.Alert, error) {
	if err := FireMiniShaiHuludAttack(ctx, c); err != nil {
		return nil, err
	}
	// Give the collector a beat to persist the alert.
	deadline := time.Now().Add(3 * time.Second)
	s := MiniShaiHuludScenario()
	for {
		alerts, err := c.GetAlerts(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range alerts {
			if a.Package == "jsonwebtoken" && a.OldVersion == "" {
				return nil, fmt.Errorf("check: jsonwebtoken package name triggered a false secret-read alert: %+v", a)
			}
			if a.Package == s.Package && a.NewVersion == s.Malicious.Version && a.Severity == "CRITICAL" {
				return &a, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("check: no CRITICAL alert for %s@%s within timeout (%d alerts total)",
				s.Package, s.Malicious.Version, len(alerts))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func verifyDashboard(ctx context.Context, c *Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("check dashboard: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("check dashboard: GET /: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return fmt.Errorf("check dashboard: read application shell: %w", err)
	}
	html := string(body)
	for _, marker := range []string{"<title>Goodman", `id="root"`, "/assets/"} {
		if !strings.Contains(html, marker) {
			return fmt.Errorf("check dashboard: built application shell missing %q", marker)
		}
	}
	return nil
}

func hasCriticalScenarioAlert(alerts []model.Alert, s Scenario) bool {
	for _, alert := range alerts {
		if alert.Service == s.Service && alert.Package == s.Package &&
			alert.OldVersion == s.Baseline.Version && alert.NewVersion == s.Malicious.Version &&
			alert.Severity == "CRITICAL" {
			return true
		}
	}
	return false
}

func hasScenarioFingerprint(fingerprints []model.Fingerprint, s Scenario) bool {
	for _, fp := range fingerprints {
		if fp.Service == s.Service && fp.Package == s.Package && fp.Version == s.Malicious.Version {
			return true
		}
	}
	return false
}
