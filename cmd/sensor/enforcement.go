// Fail-open enforcement scope and verdict reconciliation.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/hi-heisenbug/goodman/internal/coverage"
	"github.com/hi-heisenbug/goodman/internal/enforce"
	"github.com/hi-heisenbug/goodman/internal/loader"
	"github.com/hi-heisenbug/goodman/internal/model"
)

func isDenyEvent(t uint8) bool {
	switch model.EventType(t) {
	case model.EventDenyFileOpen, model.EventDenyConnect, model.EventDenyExec:
		return true
	default:
		return false
	}
}

type enforcementLoader interface {
	ReconcileEnforcement(map[uint64]string, enforce.ServiceVerdicts) error
}

type enforcementReconciler struct {
	mu       sync.Mutex
	loader   enforcementLoader
	scopes   map[uint64]string
	verdicts enforce.ServiceVerdicts
	rev      int
	haveRev  bool
	dirty    bool
}

func newEnforcementReconciler(l enforcementLoader) *enforcementReconciler {
	return &enforcementReconciler{
		loader:   l,
		scopes:   map[uint64]string{},
		verdicts: enforce.ServiceVerdicts{},
		dirty:    true,
	}
}

func (r *enforcementReconciler) setScopes(scopes map[uint64]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !sameScopes(r.scopes, scopes) {
		r.scopes = cloneScopes(scopes)
		r.dirty = true
	}
	return r.applyLocked()
}

func (r *enforcementReconciler) setVerdicts(rev int, verdicts enforce.ServiceVerdicts) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.haveRev || r.rev != rev {
		r.rev = rev
		r.haveRev = true
		r.verdicts = verdicts
		r.dirty = true
	}
	return r.applyLocked()
}

func (r *enforcementReconciler) applyLocked() error {
	if !r.dirty {
		return nil
	}
	if err := r.loader.ReconcileEnforcement(r.scopes, r.verdicts); err != nil {
		return err
	}
	r.dirty = false
	return nil
}

func sameScopes(a, b map[uint64]string) bool {
	if len(a) != len(b) {
		return false
	}
	for id, service := range a {
		if b[id] != service {
			return false
		}
	}
	return true
}

func cloneScopes(in map[uint64]string) map[uint64]string {
	out := make(map[uint64]string, len(in))
	for id, service := range in {
		out[id] = service
	}
	return out
}

func enforceScopeLoop(ctx context.Context, reconciler *enforcementReconciler, cgroupRoot, nodeName string, explicit []string) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	reconcile := func() {
		var scopes map[uint64]string
		var err error
		if len(explicit) > 0 {
			scopes, err = coverage.ResolveExplicitCgroupScopes(explicit)
		} else {
			scopes, err = coverage.ScanEnforcedCgroups(cgroupRoot, nodeName)
		}
		if err != nil {
			log.Printf("enforce scope: %v", err)
			return
		}
		if err := reconciler.setScopes(scopes); err != nil {
			log.Printf("enforce scope: %v", err)
		}
	}
	reconcile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			reconcile()
		}
	}
}

func enforcePollLoop(ctx context.Context, client *http.Client, l *loader.Loader, reconciler *enforcementReconciler, baseURL, token, sensor string) {
	const ttl = 10 * time.Second
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	poll := func() {
		u := baseURL + "/v1/enforce/state?sensor=" + urlQueryEscape(sensor) +
			"&enforcement_active=" + strconv.FormatBool(l.EnforcementActive())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return // fail-open: deadline lapses on its own
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}
		var state struct {
			Enabled  bool                    `json:"enabled"`
			Rev      int                     `json:"rev"`
			Verdicts enforce.ServiceVerdicts `json:"verdicts"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
			return
		}
		if state.Enabled {
			deadline := loader.MonotonicNowNs() + uint64(ttl.Nanoseconds())
			_ = l.SetEnforceDeadline(deadline)
		} else {
			_ = l.SetEnforceDeadline(0)
		}
		if err := reconciler.setVerdicts(state.Rev, state.Verdicts); err != nil {
			log.Printf("enforce verdicts: %v", err)
		}
	}
	poll()
	for {
		select {
		case <-ctx.Done():
			_ = l.SetEnforceDeadline(0)
			return
		case <-t.C:
			poll()
		}
	}
}

func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}
