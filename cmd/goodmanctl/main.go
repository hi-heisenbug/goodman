// goodmanctl is the dev/ops CLI entrypoint and shared HTTP plumbing.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

const usage = `goodmanctl — Goodman dev CLI

Usage:
  goodmanctl tail          [-collector URL]                stream live events + alerts
  goodmanctl alerts        [-collector URL] [-status S] [-limit N] [-offset N]
                                                            list alert pages
  goodmanctl ack ID        [-collector URL]                acknowledge an alert
  goodmanctl resolve ID    [-collector URL]                resolve an alert
  goodmanctl fingerprints  [-collector URL] [-service S] [-package P]
  goodmanctl fingerprints export [-collector URL] [-token T]
  goodmanctl fingerprints import <file> [-collector URL] [-token T]
  goodmanctl snapshot      [-collector URL] [-token T] [-o FILE]
                                                            compact open-state JSON
  goodmanctl export        [-collector URL] [-token T] [-o FILE]
                                                            complete persisted-state JSON
  goodmanctl report -lockfile package-lock.json [-service S] [-osv] [-o FILE]
                                                            runtime reachability report
  goodmanctl demo          [-host 127.0.0.1] [-port 8844] [-attack-delay 12s]
                                                            five-minute product wow (no root)
  goodmanctl enforce status|on|off [-collector URL] [-token T]
                                                            kernel enforcement runtime switch
  goodmanctl attribute -pid N [-duration 15s] [-proc-root /proc]
                                                            live-attribute one pid (needs root)

All collector commands accept -token (or GOODMAN_API_TOKEN) when the
collector's API requires authentication.
`

var errUnauthorized = errors.New("unauthorized")

var commandHandlers = map[string]func([]string){
	"tail":         cmdTail,
	"alerts":       cmdAlerts,
	"ack":          cmdAck,
	"resolve":      cmdResolve,
	"fingerprints": cmdFingerprintsRoot,
	"report":       cmdReport,
	"snapshot":     cmdSnapshot,
	"export":       cmdExport,
	"demo":         cmdDemo,
	"enforce":      cmdEnforce,
	"attribute":    cmdAttribute,
}

func main() {
	log.SetFlags(0)
	if !dispatchCommand(os.Args[1:]) {
		fmt.Print(usage)
		os.Exit(2)
	}
}

func dispatchCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	handler, ok := commandHandlers[args[0]]
	if !ok {
		return false
	}
	handler(args[1:])
	return true
}

func collectorFlag(fs *flag.FlagSet) *string {
	def := os.Getenv("GOODMAN_COLLECTOR_URL")
	if def == "" {
		def = "http://127.0.0.1:8844"
	}
	return fs.String("collector", def, "collector base URL")
}

func tokenFlag(fs *flag.FlagSet) *string {
	return fs.String("token", os.Getenv("GOODMAN_API_TOKEN"), "API bearer token (when the collector requires one)")
}

// doRequest issues an HTTP request with the API token attached and fails the
// command with a hint when the collector rejects the credentials.
func doRequest(method, u, token string) *http.Response {
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		log.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		log.Fatal("collector returned 401 unauthorized: set GOODMAN_API_TOKEN or pass -token")
	}
	return resp
}

func getJSON(u, token string, v any) {
	resp := doRequest(http.MethodGet, u, token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("collector returned %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		log.Fatal(err)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
