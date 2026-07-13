package report

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestOSVChunksLargeQuery verifies package sets larger than the 1000-query
// batch cap are split across multiple querybatch requests.
func TestOSVChunksLargeQuery(t *testing.T) {
	var batches atomic.Int64
	var maxLen atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/vulns/") {
			w.Write([]byte(`{"summary":"x","database_specific":{"severity":"HIGH"}}`))
			return
		}
		batches.Add(1)
		var body struct {
			Queries []json.RawMessage `json:"queries"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if int64(len(body.Queries)) > maxLen.Load() {
			maxLen.Store(int64(len(body.Queries)))
		}
		// No vulns for any package: keeps the test offline for /vulns.
		results := make([]map[string]any, len(body.Queries))
		for i := range results {
			results[i] = map[string]any{"vulns": []any{}}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer ts.Close()

	pkgs := make([]DeclaredPackage, 2300)
	for i := range pkgs {
		pkgs[i] = DeclaredPackage{Name: "p", Version: "1.0.0"}
	}
	c := &OSVClient{Endpoint: ts.URL}
	if _, err := c.Query(context.Background(), pkgs); err != nil {
		t.Fatal(err)
	}
	if batches.Load() != 3 { // 2300 -> 1000 + 1000 + 300
		t.Fatalf("batches = %d, want 3", batches.Load())
	}
	if maxLen.Load() > maxBatch {
		t.Fatalf("a batch had %d queries, exceeds cap %d", maxLen.Load(), maxBatch)
	}
}
