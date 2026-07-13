package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
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
	return fmt.Sprintf("http://%s:%s", o.Host, o.Port)
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
        from compromised package versions (good-pkg, axios, …)
        with rule chips (secret-read, cloud-metadata, new-exec).

10–25s  Switch to Reachability. Headline: 1,400 declared
        dependencies, 240 actually executed at runtime — the
        "shipped vs run" prioritization pitch.

25–%ds  Stay on Alerts. In ~%s the 2018 event-stream /
        flatmap-stream attack replays live: wallet.dat read +
        exfil connect. Watch the new CRITICAL row appear with
        secret-read and new-outbound-connect chips.

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

	bin := opt.CollectorBin
	if bin == "" {
		bin = "bin/collector"
	}
	absBin, err := filepath.Abs(bin)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absBin); err != nil {
		return fmt.Errorf("collector binary not found at %s (run: make build): %w", absBin, err)
	}

	db := opt.DB
	if db == "" {
		db = "demo_build/goodman_demo.db"
	}
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-shm", "-wal"} {
		_ = os.Remove(db + suffix)
	}

	listen := opt.Host + ":" + opt.Port
	cmd := exec.CommandContext(ctx, absBin, "-listen", listen)
	cmd.Env = append(os.Environ(),
		"GOODMAN_DSN="+db,
		"GOODMAN_LEARN_OBS=3",
		"GOODMAN_LEARN_MIN_AGE=1s",
	)
	cmd.Stdout = opt.Stderr
	cmd.Stderr = opt.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start collector: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			_, _ = cmd.Process.Wait()
		}
	}()

	c := NewClient(opt.URL())
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.WaitHealthy(waitCtx); err != nil {
		return err
	}

	fmt.Fprintln(opt.Stdout, "Seeding product fingerprints and alerts…")
	if err := SeedProduct(ctx, c); err != nil {
		return err
	}
	fmt.Fprintln(opt.Stdout, "Seeding event-stream baseline (flatmap-stream@0.1.0)…")
	if err := SeedEventStreamBaseline(ctx, c); err != nil {
		return err
	}
	fmt.Fprintln(opt.Stdout, "Preloading reachability report (1,400 / 240)…")
	rep, err := SeedReachability(ctx, c)
	if err != nil {
		return err
	}
	if rep.DeclaredCount != DeclaredCount || rep.ExecutedCount != ExecutedCount {
		return fmt.Errorf("reachability seed counts: declared=%d executed=%d (want %d/%d)",
			rep.DeclaredCount, rep.ExecutedCount, DeclaredCount, ExecutedCount)
	}
	fmt.Fprintln(opt.Stdout, "Seeding coverage panel (incl. unlabeled staging gap)…")
	if err := SeedCoverage(ctx, c); err != nil {
		return err
	}

	if opt.Check {
		return runCheck(ctx, c, opt)
	}

	fmt.Fprint(opt.Stdout, GuidedScript(opt.URL(), opt.AttackDelay))

	fmt.Fprintf(opt.Stdout, "\nReplaying event-stream attack in %s…\n", opt.AttackDelay)
	timer := time.NewTimer(opt.AttackDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	fmt.Fprintln(opt.Stdout, "Firing flatmap-stream@0.1.1 (wallet.dat + exfil)…")
	if err := FireEventStreamAttack(ctx, c); err != nil {
		return err
	}
	fmt.Fprintln(opt.Stdout, "Attack injected. Watch the Alerts tab for the new CRITICAL row.")

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func runCheck(ctx context.Context, c *Client, opt Options) error {
	stored, err := c.GetReport(ctx)
	if err != nil {
		return fmt.Errorf("check stored report: %w", err)
	}
	if stored.DeclaredCount != DeclaredCount || stored.ExecutedCount != ExecutedCount {
		return fmt.Errorf("stored report: declared=%d executed=%d (want %d/%d)",
			stored.DeclaredCount, stored.ExecutedCount, DeclaredCount, ExecutedCount)
	}

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

	if err := FireEventStreamAttack(ctx, c); err != nil {
		return err
	}
	// Give the collector a beat to persist the alert.
	deadline := time.Now().Add(3 * time.Second)
	s := EventStreamScenario()
	for {
		alerts, err := c.GetAlerts(ctx)
		if err != nil {
			return err
		}
		for _, a := range alerts {
			if a.Package == s.Package && a.NewVersion == s.Malicious.Version && a.Severity == "CRITICAL" {
				fmt.Fprintf(opt.Stdout, "demo check OK: reachability %d/%d, CRITICAL %s@%s→%s\n",
					stored.DeclaredCount, stored.ExecutedCount, a.Package, a.OldVersion, a.NewVersion)
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("check: no CRITICAL alert for %s@%s within timeout (%d alerts total)",
				s.Package, s.Malicious.Version, len(alerts))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
