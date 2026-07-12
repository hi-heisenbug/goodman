package api

import (
	"crypto/subtle"
	"net/http"
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
	// /v1/stream). Sent as "Authorization: Bearer <token>", or as a "token"
	// query parameter for EventSource clients that cannot set headers.
	APIToken string
}

// Enabled reports whether any endpoint is token-protected.
func (a AuthConfig) Enabled() bool { return a.IngestToken != "" || a.APIToken != "" }

// requireToken wraps h so it only runs when the request carries the expected
// bearer token. A zero-value token disables the check. allowQuery additionally
// accepts ?token= for SSE clients (EventSource cannot set request headers).
func requireToken(token string, allowQuery bool, h http.HandlerFunc) http.HandlerFunc {
	if token == "" {
		return h
	}
	want := []byte(token)
	return func(w http.ResponseWriter, r *http.Request) {
		got := bearerToken(r)
		if got == "" && allowQuery {
			got = r.URL.Query().Get("token")
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
