package actions

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Catalog prose and executor errors are both caller-facing routing instructions.
// Pin the known incident and equivalent target-platform wording, not all uses of STG.
var foreignApprovalHome = regexp.MustCompile(`(?i)(stg|dev)\.ops\.bifrost\.lan|(?:request|submit|approve)[^.\n]*(?:target runtime|that environment|platform (?:whose|that issued)|(?:stg|staging|dev) platform)`)

func TestCatalogHumanApprovalHome(t *testing.T) {
	for _, a := range Catalog() {
		if foreignApprovalHome.MatchString(a.Description) {
			t.Errorf("%s directs human approval away from ops.bifrost.lan: %s", a.ID, a.Description)
		}
	}
	// Inspect actual Go string literals in server wiring/executors, not comments/tests.
	files, err := os.ReadDir("../server")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		path := filepath.Join("../server", file.Name())
		tree, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(tree, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			if foreignApprovalHome.MatchString(value) {
				t.Errorf("%s directs human approval away from ops.bifrost.lan: %s", path, value)
			}
			return true
		})
	}
}

func TestCatalogHumanApprovalHomeDetector(t *testing.T) {
	for _, text := range []string{
		"Request on the target runtime's platform (env: stg or prod)",
		"Request it on the platform whose runtime issued the paper",
		"request no_merge on that environment's platform",
		"request the void on the platform that issued the paper",
		"Submit on the STG platform", "https://stg.ops.bifrost.lan",
	} {
		if !foreignApprovalHome.MatchString(text) {
			t.Errorf("missed %q", text)
		}
	}
	if foreignApprovalHome.MatchString("Request on the PROD platform at ops.bifrost.lan; target env may be stg.") {
		t.Fatal("rejects central approval")
	}
}
