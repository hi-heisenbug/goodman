// Alert listing and lifecycle commands.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func cmdAlerts(args []string) {
	fs := flag.NewFlagSet("alerts", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	status := fs.String("status", "", "filter by status (open|acknowledged|resolved)")
	limit := fs.Int("limit", 100, "alerts per page (1-500)")
	offset := fs.Int("offset", 0, "number of newest alerts to skip")
	asJSON := fs.Bool("json", false, "raw JSON output")
	fs.Parse(args)

	u, err := url.Parse(*collector + "/v1/alerts")
	if err != nil {
		log.Fatal(err)
	}
	q := u.Query()
	if *status != "" {
		q.Set("status", *status)
	}
	q.Set("limit", fmt.Sprint(*limit))
	q.Set("offset", fmt.Sprint(*offset))
	u.RawQuery = q.Encode()
	var alerts []model.Alert
	getJSON(u.String(), *token, &alerts)
	if *asJSON {
		json.NewEncoder(os.Stdout).Encode(alerts)
		return
	}
	if len(alerts) == 0 {
		fmt.Println("no alerts")
		return
	}
	for _, a := range alerts {
		fmt.Printf("%s  [%-8s] %-12s %s: %s %s → %s\n",
			model.NsToTime(a.DetectedAt).Format("2006-01-02 15:04:05"),
			a.Severity, a.Status, a.Service, a.Package, orDash(a.OldVersion), a.NewVersion)
		for _, b := range a.NewBehaviors {
			fmt.Printf("    + %s\n", b)
		}
		if len(a.MatchedRules) > 0 {
			fmt.Printf("    rules: %s\n", strings.Join(a.MatchedRules, ", "))
		}
		for _, ev := range a.Evidence {
			if ev.Sensor != "" {
				fmt.Printf("    seen: %s on %s at %s\n", ev.Behavior, ev.Sensor,
					model.NsToTime(ev.FirstSeen).Format("2006-01-02 15:04:05"))
			}
		}
		fmt.Printf("    id: %s\n", a.ID)
	}
}

func cmdAck(args []string) {
	postAlertAction(args, "ack", "acknowledged")
}

func cmdResolve(args []string) {
	postAlertAction(args, "resolve", "resolved")
}

func postAlertAction(args []string, action, done string) {
	fs := flag.NewFlagSet("ack", flag.ExitOnError)
	collector := collectorFlag(fs)
	token := tokenFlag(fs)
	fs.Parse(args)
	if fs.NArg() != 1 {
		log.Fatalf("usage: goodmanctl %s <alert-id>", action)
	}
	resp := doRequest(http.MethodPost, *collector+"/v1/alerts/"+url.PathEscape(fs.Arg(0))+"/"+action, *token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("collector returned %s", resp.Status)
	}
	fmt.Println(done)
}
