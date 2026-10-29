package rules

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// B-02: wide allow rules must still request core review for the actual
// transport, identity configuration and injected evidence/policy providers.
func TestReview686TrustTransportClassification(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"api/internal/cluster/client.go", "api/internal/config/platform_auth.go", "api/internal/config/clusters.go",
		"api/internal/opscontext/load.go", "api/internal/placement/new.go", "api/internal/probe/new.go",
		"api/internal/delivery/new.go", "api/internal/lineage/new.go", "api/internal/releases/new.go",
		"api/internal/agentrt/exam/new.go", "api/internal/workactions/new.go",
	} {
		t.Run(p, func(t *testing.T) {
			l, c := landingRule(t).Landing, simpleChange()
			l.Repos[0].Allow = []string{"**"}
			c.OldPath, c.NewPath = "api/internal/research/new.go", "api/internal/research/new.go"
			if got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c}); !slices.Equal(got, []LandingRefusal{"floor_soft"}) {
				t.Fatal("business control", got)
			}
			c.OldPath, c.NewPath = p, p
			if !LandingHardCore("bifrost-platform", p) || LandingHardAbsolute("bifrost-platform", p) {
				t.Errorf("trusted dependency not core: %s", p)
			}
			if got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c}); !slices.Equal(got, []LandingRefusal{"core"}) {
				t.Errorf("trusted dependency got %v, want only core", got)
			}
		})
	}
}

// No import is added here: remove coverage for an already-used dependency.
// A broad assembly exemption used to hide all these existing edges.
func TestReview686ClosureExistingTrustEdges(t *testing.T) {
	t.Parallel()
	pkgs := listDependencies(t, filepath.Join(repositoryRoot(t), "api"), "./internal/server", "./cmd/platform-api", "./cmd/operator-plane")
	protected := func(string) bool { return true }
	if got := uncoveredDependencies(pkgs, modulePath, protected, assemblyEdge); len(got) != 0 {
		t.Fatal("fully covered control", got)
	}
	for _, edge := range []struct{ from, to string }{
		{"internal/server", "internal/cluster"},
		{"internal/server", "internal/config"},
		{"cmd/platform-api", "internal/config"},
		{"cmd/operator-plane", "internal/config"},
		{"internal/cluster", "internal/probe"},
		{"internal/config", "internal/opscontext"},
		{"internal/server", "internal/delivery"},
		{"internal/server", "internal/lineage"},
		{"internal/server", "internal/releases"},
		{"internal/server", "internal/agentrt/exam"},
		{"internal/server", "internal/workactions"},
	} {
		t.Run(edge.from+"->"+edge.to, func(t *testing.T) {
			want := modulePath + edge.from + " -> " + modulePath + edge.to
			got := uncoveredDependencies(pkgs, modulePath, func(p string) bool { return p != modulePath+edge.to }, assemblyEdge)
			if !slices.Contains(got, want) {
				t.Fatalf("existing trust edge hidden: want %s in %v", want, got)
			}
		})
	}
}

// R-04: each supported source extension gets each ordinary direct spelling.
// Removing precisely that file's coverage must turn the same scan red.
func TestReview686ConsoleScanExtensionsAndHeaders(t *testing.T) {
	t.Parallel()
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"} {
		for _, source := range []string{
			"postApprovalDecision(x)", "postApprovalResolve(x)", "createApprovalRequest(x)",
			"operatorToken()", "APPROVAL_TOKEN_STORAGE_KEY", "navigator.serviceWorker.register('/sw.js')",
			"const h = { Authorization: token }", "const h = { 'Authorization': token }",
			"h.Authorization = token", "h.authorization = token", "h['Authorization'] = token",
			"const h = new Headers([['Authorization', token]])",
			"const h = new Headers({ Authorization: token })",
			"const h = new Headers(); h.set('Authorization', token)",
			"h.append('Authorization', token)", "h[\"set\"](\"Authorization\", token)",
			"h\n . Authorization\n = token", "new Headers([\n[\"Authorization\", token]\n])",
		} {
			t.Run(ext+"/"+source, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				rel := "console/src/new" + ext
				file := filepath.Join(dir, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				if got, err := scanConsole(dir, func(p string) bool { return p == rel }); err != nil || len(got) != 0 {
					t.Fatal("covered control", got, err)
				}
				if got, err := scanConsole(dir, func(string) bool { return false }); err != nil || !slices.Equal(got, []string{rel}) {
					t.Fatalf("coverage removed but scan stayed green: %v, %v", got, err)
				}
			})
		}
	}
}

func TestReview686ConsoleScanKeepsTestExclusions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, rel := range []string{"new.test.mts", "new.spec.cjs", "__tests__/new.jsx", "ordinary.ts"} {
		file := filepath.Join(dir, "console/src", rel)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		source := "postApprovalDecision(x)"
		if strings.HasPrefix(rel, "ordinary") {
			source = "const title = 'Hello'"
		}
		if err := os.WriteFile(file, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := scanConsole(dir, func(string) bool { return false }); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}
