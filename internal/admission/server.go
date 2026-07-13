package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var mutations = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "goodman_admission_requests_total",
	Help: "Admission webhook requests by result (mutated|unchanged|error).",
}, []string{"result"})

// Handler is the http.Handler for the /mutate endpoint.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/mutate", handleMutate)
	return mux
}

func handleMutate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 3<<20))
	if err != nil {
		mutations.WithLabelValues("error").Inc()
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var review AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		mutations.WithLabelValues("error").Inc()
		http.Error(w, "decode review", http.StatusBadRequest)
		return
	}
	out := Review(review)
	if out.Response != nil && out.Response.Patch != nil {
		mutations.WithLabelValues("mutated").Inc()
	} else {
		mutations.WithLabelValues("unchanged").Inc()
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("admission: encode response: %v", err)
	}
}

// Serve runs the HTTPS admission server until ctx is cancelled. Kubernetes
// requires TLS for webhooks, so cert and key are mandatory.
func Serve(ctx context.Context, addr, certFile, keyFile string, h http.Handler) error {
	if certFile == "" || keyFile == "" {
		return fmt.Errorf("admission webhook requires TLS cert and key")
	}
	srv := &http.Server{Addr: addr, Handler: h}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServeTLS(certFile, keyFile) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
