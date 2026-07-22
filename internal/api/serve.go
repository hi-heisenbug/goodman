package api

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type TLSFiles struct {
	CertFile string
	KeyFile  string
}

func Serve(ctx context.Context, addr string, handler http.Handler, tls TLSFiles) error {
	if (tls.CertFile == "") != (tls.KeyFile == "") {
		return fmt.Errorf("tls: certificate and key must both be set (cert=%q key=%q)", tls.CertFile, tls.KeyFile)
	}
	server := newHTTPServer(addr, handler)
	errCh := make(chan error, 1)
	go func() {
		if tls.CertFile != "" {
			errCh <- server.ListenAndServeTLS(tls.CertFile, tls.KeyFile)
			return
		}
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
	}
}
