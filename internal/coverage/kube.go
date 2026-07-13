package coverage

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hi-heisenbug/goodman/internal/admission"
)

const InjectLabelKey = "goodman.io/inject"

// ScanClusterCoverage lists namespaces + pods on nodeName via the in-cluster
// Kubernetes API (stdlib only) and builds injection coverage rows.
func ScanClusterCoverage(nodeName string) ([]NamespaceCoverage, error) {
	client, err := inClusterClient()
	if err != nil {
		return nil, err
	}
	nsLabels, err := listNamespaceInjectLabels(client)
	if err != nil {
		return nil, err
	}
	pods, err := listNodePods(client, nodeName)
	if err != nil {
		return nil, err
	}
	return BuildNamespaceCoverage(nsLabels, pods), nil
}

type podRow struct {
	Namespace      string
	HasNodeOptions bool
}

func BuildNamespaceCoverage(nsInject map[string]bool, pods []podRow) []NamespaceCoverage {
	type agg struct {
		inject bool
		total  int
		with   int
	}
	byNS := map[string]*agg{}
	for name, inj := range nsInject {
		byNS[name] = &agg{inject: inj}
	}
	for _, p := range pods {
		a := byNS[p.Namespace]
		if a == nil {
			a = &agg{inject: nsInject[p.Namespace]}
			byNS[p.Namespace] = a
		}
		a.total++
		if p.HasNodeOptions {
			a.with++
		}
	}
	out := make([]NamespaceCoverage, 0, len(byNS))
	for name, a := range byNS {
		// Only report namespaces that have pods on this node, or that are
		// labeled for injection (so an empty labeled ns still appears).
		if a.total == 0 && !a.inject {
			continue
		}
		out = append(out, NamespaceCoverage{
			Name:                name,
			InjectLabel:         a.inject,
			PodsTotal:           a.total,
			PodsWithNodeOptions: a.with,
			PodsWithout:         a.total - a.with,
		})
	}
	return out
}

func inClusterClient() (*http.Client, error) {
	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, fmt.Errorf("not in cluster: %w", err)
	}
	caPEM, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("invalid serviceaccount CA")
	}
	_ = token
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}, nil
}

func apiBase() (string, string, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return "", "", fmt.Errorf("kubernetes service env not set")
	}
	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("https://%s:%s", host, port), strings.TrimSpace(string(token)), nil
}

func listNamespaceInjectLabels(client *http.Client) (map[string]bool, error) {
	base, token, err := apiBase()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/namespaces", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("list namespaces: %s: %s", resp.Status, b)
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, ns := range list.Items {
		out[ns.Metadata.Name] = ns.Metadata.Labels[InjectLabelKey] == "enabled"
	}
	return out, nil
}

func listNodePods(client *http.Client, nodeName string) ([]podRow, error) {
	base, token, err := apiBase()
	if err != nil {
		return nil, err
	}
	url := base + "/api/v1/pods?fieldSelector=spec.nodeName%3D" + nodeName
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("list pods: %s: %s", resp.Status, b)
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Env []struct {
						Name  string `json:"name"`
						Value string `json:"value"`
					} `json:"env"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	out := make([]podRow, 0, len(list.Items))
	for _, p := range list.Items {
		out = append(out, podRow{
			Namespace:      p.Metadata.Namespace,
			HasNodeOptions: containerHasPerfFlags(p.Spec.Containers),
		})
	}
	return out, nil
}

func containerHasPerfFlags(containers []struct {
	Env []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"env"`
}) bool {
	for _, c := range containers {
		for _, e := range c.Env {
			if e.Name == admission.NodeOptionsEnv && strings.Contains(e.Value, admission.PerfBasicProf) {
				return true
			}
		}
	}
	return false
}
