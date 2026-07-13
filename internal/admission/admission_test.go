package admission

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// applyPatch is a minimal JSON-patch applier for the operations Mutate emits
// (add, replace), enough to assert the resulting pod env.
func decodeOps(t *testing.T, patch []byte) []map[string]any {
	t.Helper()
	var ops []map[string]any
	if err := json.Unmarshal(patch, &ops); err != nil {
		t.Fatalf("patch not JSON: %v", err)
	}
	return ops
}

func TestMutateNoEnv(t *testing.T) {
	pod := json.RawMessage(`{"spec":{"containers":[{"name":"app"}]}}`)
	patch, err := Mutate(pod)
	if err != nil {
		t.Fatal(err)
	}
	ops := decodeOps(t, patch)
	if len(ops) != 1 || ops[0]["op"] != "add" || ops[0]["path"] != "/spec/containers/0/env" {
		t.Fatalf("unexpected ops: %v", ops)
	}
	val, _ := json.Marshal(ops[0]["value"])
	if !strings.Contains(string(val), InjectedNodeOptions) {
		t.Fatalf("value missing node options: %s", val)
	}
}

func TestMutateExistingEnvNoNodeOptions(t *testing.T) {
	pod := json.RawMessage(`{"spec":{"containers":[{"name":"app","env":[{"name":"PORT","value":"3000"}]}]}}`)
	patch, err := Mutate(pod)
	if err != nil {
		t.Fatal(err)
	}
	ops := decodeOps(t, patch)
	if len(ops) != 1 || ops[0]["path"] != "/spec/containers/0/env/-" {
		t.Fatalf("expected append to env array, got %v", ops)
	}
}

func TestMutateAppendsToExistingNodeOptions(t *testing.T) {
	pod := json.RawMessage(`{"spec":{"containers":[{"name":"app","env":[
		{"name":"NODE_OPTIONS","value":"--max-old-space-size=4096"}]}]}}`)
	patch, err := Mutate(pod)
	if err != nil {
		t.Fatal(err)
	}
	ops := decodeOps(t, patch)
	if len(ops) != 1 || ops[0]["op"] != "replace" || ops[0]["path"] != "/spec/containers/0/env/0/value" {
		t.Fatalf("expected replace of existing value, got %v", ops)
	}
	v := ops[0]["value"].(string)
	if !strings.Contains(v, "--max-old-space-size=4096") || !strings.Contains(v, PerfBasicProf) || !strings.Contains(v, InterpretedNativ) {
		t.Fatalf("merged value wrong: %q", v)
	}
}

func TestMutateIdempotent(t *testing.T) {
	pod := json.RawMessage(`{"spec":{"containers":[{"name":"app","env":[
		{"name":"NODE_OPTIONS","value":"--perf-basic-prof --interpreted-frames-native-stack"}]}]}}`)
	patch, err := Mutate(pod)
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		t.Fatalf("already-injected pod must not be patched, got %s", patch)
	}
}

func TestMutateValueFromLeftAlone(t *testing.T) {
	pod := json.RawMessage(`{"spec":{"containers":[{"name":"app","env":[
		{"name":"NODE_OPTIONS","valueFrom":{"configMapKeyRef":{"name":"c","key":"k"}}}]}]}}`)
	patch, err := Mutate(pod)
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		t.Fatalf("valueFrom NODE_OPTIONS must not be rewritten, got %s", patch)
	}
}

func TestMutateMultipleContainers(t *testing.T) {
	pod := json.RawMessage(`{"spec":{"containers":[
		{"name":"a"},
		{"name":"b","env":[{"name":"X","value":"1"}]}]}}`)
	patch, err := Mutate(pod)
	if err != nil {
		t.Fatal(err)
	}
	ops := decodeOps(t, patch)
	if len(ops) != 2 {
		t.Fatalf("expected one op per container, got %v", ops)
	}
}

func TestReviewProducesBase64PatchAndUID(t *testing.T) {
	review := AdmissionReview{
		APIVersion: "admission.k8s.io/v1",
		Kind:       "AdmissionReview",
		Request: &AdmissionRequest{
			UID:    "abc-123",
			Object: json.RawMessage(`{"spec":{"containers":[{"name":"app"}]}}`),
		},
	}
	out := Review(review)
	if out.Response == nil || !out.Response.Allowed {
		t.Fatal("response must allow")
	}
	if out.Response.UID != "abc-123" {
		t.Fatalf("UID = %q, want abc-123", out.Response.UID)
	}
	if out.Response.PatchType == nil || *out.Response.PatchType != "JSONPatch" {
		t.Fatal("patchType must be JSONPatch when patched")
	}
	// On the wire the patch must be a base64 string (k8s decodes it as such).
	// Go marshals a []byte field to base64, so inspect the raw JSON, not the
	// round-tripped struct (which would already be decoded back to bytes).
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Response struct {
			Patch string `json:"patch"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	decodedPatch, err := base64.StdEncoding.DecodeString(wire.Response.Patch)
	if err != nil {
		t.Fatalf("patch is not valid base64 over the wire: %v", err)
	}
	if !strings.Contains(string(decodedPatch), InjectedNodeOptions) {
		t.Fatalf("decoded patch missing injected options: %s", decodedPatch)
	}
}

func TestReviewFailOpenOnGarbage(t *testing.T) {
	review := AdmissionReview{
		Request: &AdmissionRequest{UID: "x", Object: json.RawMessage(`not json`)},
	}
	out := Review(review)
	if out.Response == nil || !out.Response.Allowed {
		t.Fatal("must fail open (allow) on undecodable object")
	}
	if out.Response.Patch != nil {
		t.Fatal("no patch on garbage input")
	}
}

func TestHandlerEndToEnd(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	body := `{"apiVersion":"admission.k8s.io/v1","kind":"AdmissionReview",
		"request":{"uid":"u1","object":{"spec":{"containers":[{"name":"app"}]}}}}`
	resp, err := http.Post(srv.URL+"/mutate", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out AdmissionReview
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Response.UID != "u1" || out.Response.Patch == nil {
		t.Fatalf("bad response: %+v", out.Response)
	}
	// Decoding into the []byte field already base64-decoded the wire value,
	// so Patch is the raw JSON-patch bytes here.
	if !strings.Contains(string(out.Response.Patch), InjectedNodeOptions) {
		t.Fatalf("patch missing injected options: %s", out.Response.Patch)
	}
}
