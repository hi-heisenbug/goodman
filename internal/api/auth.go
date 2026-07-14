package api

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
)

// AuthConfig holds the bearer tokens that protect the collector's HTTP
// surface. Empty tokens leave the corresponding endpoints open, which is the
// local-dev default; production deployments should set both (the Helm chart
// generates them into a Secret).
type AuthConfig struct {
	// IngestToken protects POST /v1/events. Sensors send it as
	// "Authorization: Bearer <token>".
	IngestToken string
	// APIToken protects the read/mutate API (/v1/alerts, /v1/fingerprints,
	// /v1/stream). Sent as "Authorization: Bearer <token>"; browser EventSource
	// clients use the path-scoped goodman_stream_token cookie.
	APIToken string
}

const streamTokenCookie = "goodman_stream_token"

// Enabled reports whether any endpoint is token-protected.
func (a AuthConfig) Enabled() bool { return a.IngestToken != "" || a.APIToken != "" }

// requireToken wraps h so it only runs when the request carries the expected
// bearer token. A zero-value token disables the check. allowStreamCookie
// accepts the path-scoped SSE cookie for EventSource, which cannot set headers.
func requireToken(token string, allowStreamCookie bool, h http.HandlerFunc) http.HandlerFunc {
	if token == "" {
		return h
	}
	want := []byte(token)
	return func(w http.ResponseWriter, r *http.Request) {
		got := bearerToken(r)
		if got == "" && allowStreamCookie {
			if cookie, err := r.Cookie(streamTokenCookie); err == nil {
				got, _ = url.QueryUnescape(cookie.Value)
			}
		}
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="goodman"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		h(w, r)
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}
