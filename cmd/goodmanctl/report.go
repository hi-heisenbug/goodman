// Runtime reachability report command.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/hi-heisenbug/goodman/internal/model"
	"github.com/hi-heisenbug/goodman/internal/report"
)

// cmdReport builds the runtime reachability report: declared dependencies
// (from a lockfile) joined against packages Goodman observed executing, with
// optional OSV vulnerability enrichment.
func cmdReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	lockfile := fs.String("lockfile", "package-lock.json", "path to package-lock.json")
	service := fs.String("service", "", "limit to one service (pod/deployment)")
	useOSV := fs.Bool("osv", false, "enrich with OSV.dev vulnerability data (needs network)")
	out := fs.String("o", "", "write the markdown report to this file (default stdout)")
	fs.Parse(args)

	data, err := os.ReadFile(*lockfile)
	if err != nil {
		log.Fatalf("read lockfile: %v", err)
	}
	declared, err := report.ParseLockfile(data)
	if err != nil {
		log.Fatal(err)
	}
	if len(declared) == 0 {
		log.Fatalf("no packages found in %s", *lockfile)
	}

	u := *collector + "/v1/fingerprints"
	if *service != "" {
		u += "?service=" + url.QueryEscape(*service)
	}
	var fps []model.Fingerprint
	getJSON(u, *token, &fps)

	var vulns map[string][]report.Vulnerability
	if *useOSV {
		log.Printf("querying OSV.dev for %d packages…", len(declared))
		vulns, err = report.NewOSVClient().Query(context.Background(), declared)
		if err != nil {
			log.Fatalf("osv: %v", err)
		}
	}

	rep := report.Build(*service, declared, fps, vulns)
	md := rep.Markdown()
	if *out == "" {
		fmt.Print(md)
	} else if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
		log.Fatalf("write report: %v", err)
	} else {
		log.Printf("wrote %s (%d declared, %d executed)", *out, rep.DeclaredCount, rep.ExecutedCount)
	}
}
