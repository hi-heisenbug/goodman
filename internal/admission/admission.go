// Package admission implements a Kubernetes mutating admission webhook that
// injects NODE_OPTIONS and PYTHONPERFSUPPORT into pods so Tier-1 attribution
// works without any app change. The API server only calls this webhook for
// namespaces selected by the MutatingWebhookConfiguration (label
// goodman.io/inject=enabled), so the webhook injects into every pod it receives.
//
// The AdmissionReview types are hand-rolled (a tiny subset of
// admission.k8s.io/v1) to avoid pulling in the k8s.io/api dependency tree.
package admission

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The env vars and flags that enable runtime perf-map output for Tier-1
// attribution (V8 for Node, trampoline for CPython 3.12+).
const (
	NodeOptionsEnv       = "NODE_OPTIONS"
	PythonPerfSupportEnv = "PYTHONPERFSUPPORT"
	PythonPerfSupportVal = "1"
	PerfBasicProf        = "--perf-basic-prof"
	InterpretedNativ     = "--interpreted-frames-native-stack"
)

// InjectedNodeOptions is the value the webhook ensures is present.
var InjectedNodeOptions = PerfBasicProf + " " + InterpretedNativ

// AdmissionReview is the request/response envelope (admission.k8s.io/v1).
type AdmissionReview struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Request    *AdmissionRequest  `json:"request,omitempty"`
	Response   *AdmissionResponse `json:"response,omitempty"`
}

type AdmissionRequest struct {
	UID    string          `json:"uid"`
	Object json.RawMessage `json:"object"`
}

type AdmissionResponse struct {
	UID       string  `json:"uid"`
	Allowed   bool    `json:"allowed"`
	PatchType *string `json:"patchType,omitempty"`
	Patch     []byte  `json:"patch,omitempty"` // base64-encoded JSON patch (json.Marshal handles []byte)
}

// patchOp is one RFC-6902 JSON Patch operation.
type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

type envVar struct {
	Name      string          `json:"name"`
	Value     string          `json:"value,omitempty"`
	ValueFrom json.RawMessage `json:"valueFrom,omitempty"`
}

// Mutate returns the JSON-patch operations that ensure NODE_OPTIONS carries
// the perf-map flags and PYTHONPERFSUPPORT=1 is set on every container in the
// pod object. It is pure and unit-testable: no network, no cluster. Returns
// nil when nothing needs to change (idempotent re-admission).
func Mutate(podObject json.RawMessage) ([]byte, error) {
	type container struct {
		Env []envVar `json:"env"`
	}
	var pod struct {
		Spec struct {
			Containers     []container `json:"containers"`
			InitContainers []container `json:"initContainers"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(podObject, &pod); err != nil {
		return nil, fmt.Errorf("decode pod: %w", err)
	}

	var ops []patchOp
	// Node/Python workloads may run in regular containers or in initContainers
	// (migrations, warmups); both need the env injection for attribution.
	add := func(field string, containers []container) {
		for i, c := range containers {
			ops = append(ops, containerOps(field, i, c.Env)...)
		}
	}
	add("containers", pod.Spec.Containers)
	add("initContainers", pod.Spec.InitContainers)

	if len(ops) == 0 {
		return nil, nil
	}
	return json.Marshal(ops)
}

// containerOps returns the patch ops that ensure NODE_OPTIONS and
// PYTHONPERFSUPPORT for one container at /spec/<field>/<i>.
func containerOps(field string, i int, env []envVar) []patchOp {
	needNode, needPy := true, true
	if idx, cur := findEnv(env, NodeOptionsEnv); idx >= 0 {
		if cur.ValueFrom != nil {
			needNode = false // cannot merge valueFrom
		} else if appendFlags(cur.Value) == cur.Value {
			needNode = false
		}
	}
	if idx, cur := findEnv(env, PythonPerfSupportEnv); idx >= 0 {
		// Any existing value (including "0") or valueFrom is an operator
		// choice — leave untouched.
		_ = cur
		needPy = false
	}

	if !needNode && !needPy {
		return nil
	}

	if len(env) == 0 {
		var vars []envVar
		if needNode {
			vars = append(vars, envVar{Name: NodeOptionsEnv, Value: InjectedNodeOptions})
		}
		if needPy {
			vars = append(vars, envVar{Name: PythonPerfSupportEnv, Value: PythonPerfSupportVal})
		}
		return []patchOp{{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/%s/%d/env", field, i),
			Value: vars,
		}}
	}

	var ops []patchOp
	if needNode {
		if idx, cur := findEnv(env, NodeOptionsEnv); idx >= 0 {
			ops = append(ops, patchOp{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/%s/%d/env/%d/value", field, i, idx),
				Value: appendFlags(cur.Value),
			})
		} else {
			ops = append(ops, patchOp{
				Op:    "add",
				Path:  fmt.Sprintf("/spec/%s/%d/env/-", field, i),
				Value: envVar{Name: NodeOptionsEnv, Value: InjectedNodeOptions},
			})
		}
	}
	if needPy {
		ops = append(ops, patchOp{
			Op:    "add",
			Path:  fmt.Sprintf("/spec/%s/%d/env/-", field, i),
			Value: envVar{Name: PythonPerfSupportEnv, Value: PythonPerfSupportVal},
		})
	}
	return ops
}

func findEnv(env []envVar, name string) (int, envVar) {
	for i, e := range env {
		if e.Name == name {
			return i, e
		}
	}
	return -1, envVar{}
}

// appendFlags adds any missing perf-map flag to an existing NODE_OPTIONS
// value without duplicating flags already present.
func appendFlags(existing string) string {
	fields := strings.Fields(existing)
	have := map[string]bool{}
	for _, f := range fields {
		have[f] = true
	}
	for _, flag := range []string{PerfBasicProf, InterpretedNativ} {
		if !have[flag] {
			fields = append(fields, flag)
		}
	}
	return strings.Join(fields, " ")
}

// Review handles one AdmissionReview request and returns the response review.
// A decode failure still returns an allowed response (fail-open): a webhook
// must never block unrelated workloads from scheduling.
func Review(review AdmissionReview) AdmissionReview {
	resp := &AdmissionResponse{Allowed: true}
	out := AdmissionReview{APIVersion: review.APIVersion, Kind: review.Kind, Response: resp}
	if review.Request == nil {
		return out
	}
	resp.UID = review.Request.UID

	patch, err := Mutate(review.Request.Object)
	if err != nil || patch == nil {
		return out // allow unchanged
	}
	pt := "JSONPatch"
	resp.PatchType = &pt
	resp.Patch = patch
	return out
}
