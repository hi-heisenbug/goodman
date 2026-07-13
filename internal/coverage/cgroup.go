package coverage

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

func cgroupDirID(path string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return st.Ino, nil
}

// ScanEnforcedCgroups lists enforce-labeled namespaces and resolves pod cgroup
// ids on this node. Errors on individual pods are skipped (fail-open).
func ScanEnforcedCgroups(cgroupRoot, nodeName string) (map[uint64]bool, error) {
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
	out := map[uint64]bool{}
	for _, p := range pods {
		if !nsEnforce[p.Namespace] {
			continue
		}
		ids, err := CgroupIDsForPodUID(cgroupRoot, p.UID)
		if err != nil {
			continue
		}
		for _, id := range ids {
			out[id] = true
		}
	}
	return out, nil
}

func listNamespaceEnforceLabels(client *http.Client) (map[string]bool, error) {
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
		out[ns.Metadata.Name] = ns.Metadata.Labels[EnforceLabelKey] == "enabled"
	}
	return out, nil
}

type podRowWithUID struct {
	Namespace string
	UID       string
}

func listNodePodsWithUID(client *http.Client, nodeName string) ([]podRowWithUID, error) {
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
				UID       string `json:"uid"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	out := make([]podRowWithUID, 0, len(list.Items))
	for _, p := range list.Items {
		out = append(out, podRowWithUID{Namespace: p.Metadata.Namespace, UID: p.Metadata.UID})
	}
	return out, nil
}

// ResolveCgroupPaths resolves explicit cgroup2 directory paths to ids (e2e/lab).
func ResolveCgroupPaths(paths []string) map[uint64]bool {
	out := map[uint64]bool{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		_ = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if id, err := cgroupDirID(path); err == nil {
				out[id] = true
			}
			return nil
		})
	}
	return out
}
