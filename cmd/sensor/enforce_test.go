package main

import (
	"errors"
	"net/url"
	"testing"

	"github.com/hi-heisenbug/goodman/internal/enforce"
)

type fakeEnforcementLoader struct {
	calls    int
	failNext bool
	scopes   map[uint64]string
	verdicts enforce.ServiceVerdicts
}

func TestEnforcementSensorQueryValueUsesStandardEscaping(t *testing.T) {
	value := "node a&zone=west/1"
	if got, want := urlQueryEscape(value), url.QueryEscape(value); got != want {
		t.Fatalf("urlQueryEscape(%q) = %q, want %q", value, got, want)
	}
}

func (f *fakeEnforcementLoader) ReconcileEnforcement(scopes map[uint64]string, verdicts enforce.ServiceVerdicts) error {
	f.calls++
	if f.failNext {
		f.failNext = false
		return errors.New("map full")
	}
	f.scopes = cloneScopes(scopes)
	f.verdicts = verdicts
	return nil
}

func TestEnforcementReconcilerAppliesScopeAndVerdictChanges(t *testing.T) {
	loader := &fakeEnforcementLoader{}
	reconciler := newEnforcementReconciler(loader)
	if err := reconciler.setScopes(map[uint64]string{11: "checkout"}); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.setVerdicts(2, enforce.ServiceVerdicts{
		"checkout": {Open: []string{"/etc/shadow"}},
	}); err != nil {
		t.Fatal(err)
	}
	if loader.scopes[11] != "checkout" || loader.verdicts["checkout"].Open[0] != "/etc/shadow" {
		t.Fatalf("applied scopes=%v verdicts=%v", loader.scopes, loader.verdicts)
	}
	calls := loader.calls
	if err := reconciler.setVerdicts(2, enforce.ServiceVerdicts{}); err != nil {
		t.Fatal(err)
	}
	if loader.calls != calls {
		t.Fatalf("unchanged revision reconciled again: calls=%d want=%d", loader.calls, calls)
	}
}

func TestEnforcementReconcilerRetriesFailedApply(t *testing.T) {
	loader := &fakeEnforcementLoader{failNext: true}
	reconciler := newEnforcementReconciler(loader)
	if err := reconciler.setScopes(map[uint64]string{11: "checkout"}); err == nil {
		t.Fatal("first reconciliation must fail")
	}
	if err := reconciler.setScopes(map[uint64]string{11: "checkout"}); err != nil {
		t.Fatalf("unchanged dirty state was not retried: %v", err)
	}
	if loader.calls != 2 {
		t.Fatalf("calls = %d, want 2", loader.calls)
	}
}
