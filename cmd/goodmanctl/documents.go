// Versioned snapshot and complete-state export commands.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func cmdSnapshot(args []string) {
	cmdCollectorDocument("snapshot", "/v1/snapshot", args)
}

func cmdExport(args []string) {
	cmdCollectorDocument("export", "/v1/export", args)
}

func cmdCollectorDocument(name, path string, args []string) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	outPath := fs.String("o", "", "write JSON to this file (default stdout)")
	fs.Parse(args)

	var body bytes.Buffer
	if err := fetchCollectorJSON(*collector, *token, path, &body); err != nil {
		if errors.Is(err, errUnauthorized) {
			log.Fatal("collector returned 401 unauthorized: set GOODMAN_API_TOKEN or pass -token")
		}
		log.Fatal(err)
	}
	if *outPath == "" {
		if _, err := io.Copy(os.Stdout, &body); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(*outPath, body.Bytes(), 0o600); err != nil {
		log.Fatalf("write %s: %v", name, err)
	}
}

func fetchCollectorJSON(collector, token, path string, out io.Writer) error {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(collector, "/")+path, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("collector returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	_, err = io.Copy(out, resp.Body)
	return err
}
