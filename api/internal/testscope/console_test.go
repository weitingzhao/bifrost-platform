package testscope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func consoleRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for p, content := range map[string]string{
		"console/src/pages/__tests__/QuestionPage.test.tsx": "readFile('../../../../api/internal/questions/testdata/contract/answer-request.json')",
		"console/src/Widget.tsx":                            "export const Widget = 1",
		"console/src/name.test.ts":                          "readFile('unique.go')",
	} {
		path := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestConsoleSelection(t *testing.T) {
	root := consoleRepo(t)
	for _, tc := range []struct {
		name, input, scope, related, tests, lint string
		build                                    bool
	}{
		{name: "Go only", input: "M\tapi/internal/unrelated.go", scope: "none"},
		{name: "fixture reader", input: "M\tapi/internal/questions/testdata/contract/answer-request.json", scope: "incremental", tests: "console/src/pages/__tests__/QuestionPage.test.tsx"},
		{name: "filename reader", input: "M\tapi/internal/unique.go", scope: "incremental", tests: "console/src/name.test.ts"},
		{name: "component", input: "M\tconsole/src/Widget.tsx", scope: "incremental", related: "console/src/Widget.tsx", lint: "console/src/Widget.tsx", build: true},
		{name: "package", input: "M\tconsole/package.json", scope: "full", build: true},
		{name: "malformed", input: "not a diff", scope: "full", build: true},
		{name: "rename", input: "R100\tconsole/src/Old.tsx\tconsole/src/Widget.tsx", scope: "incremental", related: "console/src/Old.tsx console/src/Widget.tsx", lint: "console/src/Widget.tsx", build: true},
		{name: "deleted", input: "D\tconsole/src/Old.tsx", scope: "incremental", related: "console/src/Old.tsx", build: true},
		{name: "config", input: "M\tconsole/vite.config.ts", scope: "full", build: true},
		{name: "docs", input: "M\tdocs/unrelated.md", scope: "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := SelectConsoleChanges(root, strings.NewReader(tc.input))
			if r.Scope != tc.scope || r.Related != tc.related || r.Tests != tc.tests || r.Lint != tc.lint || r.Build != tc.build {
				t.Fatalf("got %+v; want %+v", r, tc)
			}
		})
	}
}
func TestConsoleScanFailure(t *testing.T) {
	if r := SelectConsole(t.TempDir(), nil); r.Scope != "full" {
		t.Fatalf("got %+v", r)
	}
}
