// Enforcement runtime-switch command.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

func cmdEnforce(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: goodmanctl enforce status|on|off")
	}
	action := args[0]
	fs := flag.NewFlagSet("enforce", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	fs.Parse(args[1:])
	var method, path string
	switch action {
	case "status":
		method, path = http.MethodGet, "/v1/enforce"
	case "on":
		method, path = http.MethodPost, "/v1/enforce/on"
	case "off":
		method, path = http.MethodPost, "/v1/enforce/off"
	default:
		log.Fatal("usage: goodmanctl enforce status|on|off")
	}
	resp := doRequest(method, *collector+path, *token)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("collector returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	fmt.Println(string(body))
}
