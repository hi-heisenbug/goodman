package coverage

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const EnforceLabelKey = "goodman.io/enforce"

// CgroupIDsForPodUID walks cgroup2 under root and returns inode numbers for
// every cgroup directory whose path contains the pod UID (kubelet layout).
func CgroupIDsForPodUID(cgroupRoot, podUID string) ([]uint64, error) {
	if podUID == "" {
		return nil, nil
	}
	needle := "pod" + strings.ReplaceAll(podUID, "-", "_")
	var ids []uint64
	err := filepath.WalkDir(cgroupRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if !strings.Contains(path, needle) {
			return nil
		}
		id, err := cgroupDirID(path)
		if err != nil {
			return nil
		}
		ids = append(ids, id)
		return nil
	})
	return ids, err
}

// ScanEnforcedCgroups lists enforce-labeled namespaces and resolves each local
// pod cgroup id to the pod name Goodman uses as its service identity. Errors on
// individual pods are skipped (fail-open).
func ScanEnforcedCgroups(cgroupRoot, nodeName string) (map[uint64]string, error) {
	client, err := inClusterClient()
	if err != nil {
		return nil, err
	}
	nsEnforce, err := listNamespaceEnforceLabels(client)
	if err != nil {
		return nil, err
	}
	pods, err := listNodePodsWithUID(client, nodeName)
	if err != nil {
		return nil, err
	}
	return buildEnforcedCgroupScopes(nsEnforce, pods, func(_ string, uid string) ([]uint64, error) {
		return CgroupIDsForPodUID(cgroupRoot, uid)
	}), nil
}

func buildEnforcedCgroupScopes(
	nsEnforce map[string]bool,
	pods []podRowWithUID,
	resolve func(service, uid string) ([]uint64, error),
) map[uint64]string {
	out := map[uint64]string{}
	for _, p := range pods {
		if !nsEnforce[p.Namespace] {
			continue
		}
		service := p.serviceName()
		ids, err := resolve(service, p.UID)
		if err != nil {
			continue
		}
		for _, id := range ids {
			if existing := out[id]; existing != "" && existing != service {
				delete(out, id)
				continue
			}
			out[id] = service
		}
	}
	return out
}

func listNamespaceEnforceLabels(client *http.Client) (map[string]bool, error) {
	return listNamespaceLabelState(client, EnforceLabelKey)
}

type podRowWithUID struct {
	Namespace string
	Name      string
	Hostname  string
	UID       string
}

func (p podRowWithUID) serviceName() string {
	if p.Hostname != "" {
		return p.Hostname
	}
	return p.Name
}

func listNodePodsWithUID(client *http.Client, nodeName string) ([]podRowWithUID, error) {
	var list struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
				UID       string `json:"uid"`
			} `json:"metadata"`
			Spec struct {
				Hostname string `json:"hostname"`
			} `json:"spec"`
		} `json:"items"`
	}
	path := "/api/v1/pods?fieldSelector=spec.nodeName%3D" + nodeName
	if err := getKubernetesJSON(client, path, "list pods", &list); err != nil {
		return nil, err
	}
	out := make([]podRowWithUID, 0, len(list.Items))
	for _, p := range list.Items {
		out = append(out, podRowWithUID{
			Namespace: p.Metadata.Namespace,
			Name:      p.Metadata.Name,
			Hostname:  p.Spec.Hostname,
			UID:       p.Metadata.UID,
		})
	}
	return out, nil
}

// ResolveExplicitCgroupScopes resolves SERVICE=PATH entries used by local/e2e
// runs. Requiring the service name prevents a local scope from accidentally
// receiving another service's verdicts.
func ResolveExplicitCgroupScopes(entries []string) (map[uint64]string, error) {
	out := map[uint64]string{}
	for _, entry := range entries {
		service, path, ok := strings.Cut(entry, "=")
		service = strings.TrimSpace(service)
		path = strings.TrimSpace(path)
		if !ok || service == "" || path == "" {
			return nil, fmt.Errorf("enforce cgroup %q must be SERVICE=PATH", entry)
		}
		err := filepath.WalkDir(path, func(current string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			id, err := cgroupDirID(current)
			if err != nil {
				return err
			}
			if existing := out[id]; existing != "" && existing != service {
				return fmt.Errorf("cgroup %d assigned to both %q and %q", id, existing, service)
			}
			out[id] = service
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("resolve enforce cgroup %q: %w", entry, err)
		}
	}
	return out, nil
}
