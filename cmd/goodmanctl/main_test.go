package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDispatchCommandRoutesArguments(t *testing.T) {
	const name = "test-command"
	var got []string
	commandHandlers[name] = func(args []string) { got = append([]string(nil), args...) }
	defer delete(commandHandlers, name)

	if !dispatchCommand([]string{name, "one", "two"}) {
		t.Fatal("known command was not dispatched")
	}
	if strings.Join(got, ",") != "one,two" {
		t.Fatalf("arguments = %v", got)
	}
	if dispatchCommand([]string{"not-a-command"}) {
		t.Fatal("unknown command was accepted")
	}
	if dispatchCommand(nil) {
		t.Fatal("empty command was accepted")
	}
}

func TestFetchSnapshotWritesAuthenticatedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/snapshot" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer api-secret" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema":"goodman.snapshot/v1","alerts":[],"fingerprints":[]}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	if err := fetchCollectorJSON(srv.URL+"/", "api-secret", "/v1/snapshot", &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), `{"schema":"goodman.snapshot/v1","alerts":[],"fingerprints":[]}`; got != want {
		t.Fatalf("snapshot = %q, want %q", got, want)
	}
}

func TestFetchSnapshotReturnsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	var out bytes.Buffer
	if err := fetchCollectorJSON(srv.URL, "", "/v1/snapshot", &out); err == nil {
		t.Fatal("expected non-200 error")
	}
}

func TestFetchExportWritesAuthenticatedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/export" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer api-secret" {
			t.Errorf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"schema":"goodman.export/v1","alerts":[],"fingerprints":[]}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	if err := fetchCollectorJSON(srv.URL, "api-secret", "/v1/export", &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"goodman.export/v1"`)) {
		t.Fatalf("export = %q", out.String())
	}
}
