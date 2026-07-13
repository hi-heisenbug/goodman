package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hi-heisenbug/goodman/internal/store"
)

func newAuthedServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := NewServer(st, nil, nil)
	srv.Auth = AuthConfig{IngestToken: "ingest-secret", APIToken: "api-secret"}
	return srv
}

func TestAuthRejectsAndAccepts(t *testing.T) {
	router := newAuthedServer(t).Router(nil)

	cases := []struct {
		name   string
		method string
		path   string
		header string
		query  string
		want   int
	}{
		{"ingest no token", http.MethodPost, "/v1/events", "", "", http.StatusUnauthorized},
		{"ingest wrong token", http.MethodPost, "/v1/events", "Bearer nope", "", http.StatusUnauthorized},
		{"ingest api token rejected", http.MethodPost, "/v1/events", "Bearer api-secret", "", http.StatusUnauthorized},
		{"ingest right token", http.MethodPost, "/v1/events", "Bearer ingest-secret", "", http.StatusBadRequest}, // auth passes, empty body fails
		{"alerts no token", http.MethodGet, "/v1/alerts", "", "", http.StatusUnauthorized},
		{"alerts right token", http.MethodGet, "/v1/alerts", "Bearer api-secret", "", http.StatusOK},
		{"alerts query token ignored", http.MethodGet, "/v1/alerts", "", "token=api-secret", http.StatusUnauthorized},
		{"fingerprints right token", http.MethodGet, "/v1/fingerprints", "Bearer api-secret", "", http.StatusOK},
		{"coverage get no token", http.MethodGet, "/v1/coverage", "", "", http.StatusUnauthorized},
		{"coverage get right token", http.MethodGet, "/v1/coverage", "Bearer api-secret", "", http.StatusOK},
		{"coverage post ingest token", http.MethodPost, "/v1/coverage", "Bearer ingest-secret", "", http.StatusBadRequest},
		{"ack no token", http.MethodPost, "/v1/alerts/x/ack", "", "", http.StatusUnauthorized},
		{"healthz open", http.MethodGet, "/v1/healthz", "", "", http.StatusOK},
		{"readyz open", http.MethodGet, "/v1/readyz", "", "", http.StatusOK},
		{"metrics open", http.MethodGet, "/metrics", "", "", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := tc.path
			if tc.query != "" {
				u += "?" + tc.query
			}
			req := httptest.NewRequest(tc.method, u, strings.NewReader(""))
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("%s %s = %d, want %d (body %s)", tc.method, u, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// The SSE stream must accept ?token= because EventSource cannot set headers.
func TestStreamQueryToken(t *testing.T) {
	router := newAuthedServer(t).Router(nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/stream?token=wrong", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong query token = %d, want 401", rec.Code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req = httptest.NewRequest(http.MethodGet, "/v1/stream?token=api-secret", nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req) // returns when ctx times out
	if rec.Code != http.StatusOK {
		t.Fatalf("right query token = %d, want 200", rec.Code)
	}
}

func TestAuthDisabledWhenTokensEmpty(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "open.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	router := NewServer(st, nil, nil).Router(nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/alerts", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("open server /v1/alerts = %d, want 200", rec.Code)
	}
}
