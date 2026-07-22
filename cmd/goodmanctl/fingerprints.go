// Fingerprint inspection, export, and import commands.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func cmdFingerprintsRoot(args []string) {
	if len(args) == 0 {
		cmdFingerprints(args)
		return
	}
	switch args[0] {
	case "export":
		cmdFingerprintsExport(args[1:])
	case "import":
		cmdFingerprintsImport(args[1:])
	default:
		cmdFingerprints(args)
	}
}

func cmdFingerprints(args []string) {
	fs := flag.NewFlagSet("fingerprints", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	service := fs.String("service", "", "filter by service")
	pkg := fs.String("package", "", "filter by package")
	asJSON := fs.Bool("json", false, "raw JSON output")
	fs.Parse(args)

	u := *collector + "/v1/fingerprints?service=" + url.QueryEscape(*service) + "&package=" + url.QueryEscape(*pkg)
	var fps []model.Fingerprint
	getJSON(u, *token, &fps)
	if *asJSON {
		json.NewEncoder(os.Stdout).Encode(fps)
		return
	}
	for _, fp := range fps {
		state := "learning"
		if fp.IsBaseline {
			state = "BASELINE"
		}
		fmt.Printf("%s / %s@%s  [%s]  obs=%d  behaviors=%d\n",
			fp.Service, fp.Package, orDash(fp.Version), state, fp.ObsCount, len(fp.Behaviors))
		keys := make([]string, 0, len(fp.Behaviors))
		for b := range fp.Behaviors {
			keys = append(keys, b)
		}
		sort.Strings(keys)
		for _, b := range keys {
			st := fp.Behaviors[b]
			fmt.Printf("    %-60s x%d\n", b, st.Count)
		}
	}
}

func cmdFingerprintsExport(args []string) {
	fs := flag.NewFlagSet("fingerprints export", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	fs.Parse(args)

	resp := doRequest(http.MethodGet, *collector+"/v1/fingerprints/export", *token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("collector returned %s", resp.Status)
	}
	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		log.Fatal(err)
	}
}

func cmdFingerprintsImport(args []string) {
	fs := flag.NewFlagSet("fingerprints import", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	fs.Parse(args)
	if fs.NArg() != 1 {
		log.Fatal("usage: goodmanctl fingerprints import <file>")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		log.Fatalf("read file: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, *collector+"/v1/fingerprints/import", strings.NewReader(string(data)))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if *token != "" {
		req.Header.Set("Authorization", "Bearer "+*token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		log.Fatal("collector returned 401 unauthorized: set GOODMAN_API_TOKEN or pass -token")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("collector returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		Imported           int `json:"imported"`
		SkippedLocal       int `json:"skipped_local"`
		Replaced           int `json:"replaced"`
		IgnoredNonBaseline int `json:"ignored_non_baseline"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("imported=%d skipped_local=%d replaced=%d ignored_non_baseline=%d\n",
		result.Imported, result.SkippedLocal, result.Replaced, result.IgnoredNonBaseline)
}
