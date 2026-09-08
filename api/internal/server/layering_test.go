package server_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The out-of-band operator plane (L-1) is the tier that repairs L0–L2. Its whole
// value is that it never shares fate with what it services, which is only
// possible while its code can run with no cluster at all: no client-go, no
// kubeconfig, no internal/cluster.
//
// That is true today — every package below imports none of them — and it is the
// precondition for ever running this tier beside the remediation runners on the
// Mac minis instead of inside the thing it is meant to rescue. One import is
// enough to lose it silently, so it is asserted rather than remembered.
//
// Authority: console/src/lib/architecture/cicdBootstrapCatalog.ts, layer L-1.
var operatorPlanePackages = []string{
	"agentbridge",
	"agentdeploy",
	"agentgovernance",
	"agentreport",
	"driftproposal",
	"hermesgateway",
	"hermesinsight",
	"hermesreadiness",
	"patrol",
	"remediation",
}

var clusterBoundImports = []string{
	"bifrost-platform/api/internal/cluster",
	"k8s.io/",
}

func TestOperatorPlaneStaysClusterFree(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range operatorPlanePackages {
		dir := filepath.Join(root, pkg)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("operator-plane package %s is gone — update this list with the reason", pkg)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			for _, banned := range clusterBoundImports {
				if strings.Contains(string(data), `"`+banned) || strings.Contains(string(data), banned+`"`) {
					t.Errorf("%s/%s imports %s — the operator plane must run with no cluster; "+
						"reach the cluster through an interface the caller supplies instead",
						pkg, name, banned)
				}
			}
		}
	}
}
