package coverage

import "testing"

func TestBuildNamespaceCoverageGaps(t *testing.T) {
	ns := map[string]bool{
		"prod":     true,
		"staging":  false,
		"kube-sys": false,
	}
	pods := []podRow{
		{Namespace: "prod", HasNodeOptions: true},
		{Namespace: "prod", HasNodeOptions: false},
		{Namespace: "staging", HasNodeOptions: false},
		{Namespace: "staging", HasNodeOptions: false},
	}
	rows := BuildNamespaceCoverage(ns, pods)
	byName := map[string]NamespaceCoverage{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	if _, ok := byName["kube-sys"]; ok {
		t.Fatal("empty unlabeled namespaces should be omitted")
	}
	st := byName["staging"]
	if st.InjectLabel || st.PodsWithout != 2 || st.PodsTotal != 2 {
		t.Fatalf("staging gap = %+v", st)
	}
	pr := byName["prod"]
	if !pr.InjectLabel || pr.PodsWithout != 1 {
		t.Fatalf("prod = %+v", pr)
	}
}
