// Leader-elected collector maintenance loops.
package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/hi-heisenbug/goodman/internal/digest"
	"github.com/hi-heisenbug/goodman/internal/notify"
	"github.com/hi-heisenbug/goodman/internal/report"
	"github.com/hi-heisenbug/goodman/internal/store"
)

// pruneLoop deletes resolved alerts older than the retention window, once at
// startup and then hourly.
func pruneLoop(ctx context.Context, st *store.Store, retention time.Duration) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		pruneCtx, cancel := context.WithTimeout(ctx, time.Minute)
		err := st.WithLeader(pruneCtx, store.LockRetention, func(runCtx context.Context) error {
			n, err := st.PruneResolvedAlerts(runCtx, time.Now().Add(-retention))
			switch {
			case err != nil && ctx.Err() == nil:
				log.Printf("retention: prune failed: %v", err)
			case n > 0:
				log.Printf("retention: pruned %d resolved alerts older than %s", n, retention)
			}
			return err
		})
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("retention: leader lock: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// reachabilityLoop recomputes stored reachability snapshots against the latest
// fingerprints, once at startup and then every interval, so the dashboard
// shows current numbers without a manual re-upload.
func reachabilityLoop(ctx context.Context, st *store.Store, interval time.Duration, osvClient *report.OSVClient) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		func() {
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			err := st.WithLeader(runCtx, store.LockReachability, func(leaderCtx context.Context) error {
				lockfiles, err := st.ListLockfiles(leaderCtx)
				if err != nil {
					if ctx.Err() == nil {
						log.Printf("reachability: list lockfiles: %v", err)
					}
					return err
				}
				if len(lockfiles) == 0 {
					return nil
				}
				refresh := make([]report.Lockfile, len(lockfiles))
				for i, lf := range lockfiles {
					refresh[i] = report.Lockfile{Service: lf.Service, Content: lf.Content}
				}
				n, err := report.RefreshAll(leaderCtx, st, refresh, osvClient, uint64(time.Now().UnixNano()))
				if err != nil && ctx.Err() == nil {
					log.Printf("reachability: refresh: %v", err)
				} else if n > 0 {
					log.Printf("reachability: refreshed %d service report(s)", n)
				}
				return err
			})
			if err != nil && ctx.Err() == nil {
				log.Printf("reachability: leader lock: %v", err)
			}
		}()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// digestLoop posts a weekly heartbeat to the configured webhook once at
// startup and then every interval, so a quiet POV still speaks on day one.
func digestLoop(ctx context.Context, st *store.Store, n *notify.Notifier, interval time.Duration, budget int, publicURL string) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		func() {
			runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			err := st.WithLeader(runCtx, store.LockDigest, func(leaderCtx context.Context) error {
				d, err := digest.Build(leaderCtx, st, budget, publicURL)
				if err != nil {
					if ctx.Err() == nil {
						log.Printf("digest: build: %v", err)
					}
					return err
				}
				var payload []byte
				if n.Format() == notify.FormatSlack {
					payload, err = json.Marshal(d.SlackPayload())
				} else {
					payload, err = json.Marshal(d.GenericPayload())
				}
				if err != nil {
					log.Printf("digest: encode: %v", err)
					return err
				}
				if err := n.PostJSON(leaderCtx, payload); err != nil && ctx.Err() == nil {
					log.Printf("digest: deliver: %v", err)
					return err
				}
				log.Printf("digest: delivered (open alerts=%d, executed=%d)", d.OpenAlerts, d.ExecutedCount)
				return nil
			})
			if err != nil && ctx.Err() == nil {
				log.Printf("digest: leader lock: %v", err)
			}
		}()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
