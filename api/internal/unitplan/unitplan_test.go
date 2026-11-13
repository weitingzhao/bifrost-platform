package unitplan

import (
	"crypto/sha1" // #nosec G505 -- stands in for git's object ids in a test tree
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// testTree builds a Tree from files by path; a directory's id is the hash of
// what is under it, as git's is.
func testTree(t *testing.T, files map[string]string) *Tree {
	t.Helper()
	id := func(s string) string { h := sha1.Sum([]byte(s)); return hex.EncodeToString(h[:]) } // #nosec G401
	blobs := map[string]string{}
	dirs := map[string][]string{}
	var entries []Entry
	for p, body := range files {
		blobs[id("blob:"+body)] = body
		entries = append(entries, Entry{Path: p, ID: id("blob:" + body)})
		for d := p; strings.Contains(d, "/"); {
			d = d[:strings.LastIndex(d, "/")]
			dirs[d] = append(dirs[d], p+"="+body)
		}
		dirs[""] = append(dirs[""], p+"="+body)
	}
	dirID := func(d string) string { sort.Strings(dirs[d]); return id("tree:" + strings.Join(dirs[d], "\n")) }
	for d := range dirs {
		if d != "" {
			entries = append(entries, Entry{Path: d, Dir: true, ID: dirID(d)})
		}
	}
	tree, err := NewTree(dirID(""), entries, func(i string) ([]byte, error) {
		b, ok := blobs[i]
		if !ok {
			return nil, fmt.Errorf("no blob %s", i)
		}
		return []byte(b), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

const goMod = "module example.com/m\n\ngo 1.25\n"

func moduleFiles() map[string]string {
	return map[string]string{
		"api/go.mod": goMod, "api/go.sum": "sum-1\n",
		"api/a/a.go":         "package a\n\nimport (\n\t_ \"embed\"\n\t\"fmt\"\n\n\t\"example.com/m/b\"\n)\n\n//go:embed data/*.txt all:static\nvar _ = fmt.Sprint(b.B)\n",
		"api/a/a_windows.go": "package a\n\nimport _ \"example.com/m/onlywindows\"\n",
		"api/a/a_test.go":    "package a\n\nimport _ \"example.com/m/c\"\n",
		"api/a/x_test.go":    "package a_test\n\nimport _ \"example.com/m/d\"\n",
		"api/a/data/one.txt": "1", "api/a/data/skip.md": "no",
		"api/a/static/_x/y.txt": "y", "api/a/static/z.txt": "z",
		"api/a/testdata/g.json": "{}", "api/a/a.lock": "lock-1",
		"api/b/b.go": "package b\n\nconst B = 1\n",
		"api/c/c.go": "package c\n",
		"api/d/d.go": "package d\n",
		"api/e/e.go": "package e\n", "api/e/e_test.go": "package e\n",
		"api/onlytests/x_test.go": "package onlytests\n",
		"api/vendor/v/v.go":       "package v\n", "api/a/testdata/src/t.go": "package t\n", "api/nested/go.mod": "module other\n", "api/nested/n.go": "package n\n",
		"docs/readme.md": "hello",
	}
}

func names(files []GoFile) string {
	var out []string
	for _, f := range files {
		out = append(out, f.Name)
	}
	return strings.Join(out, " ")
}

// A module's packages as go list would report them: the files a build
// constraint excludes are still the package's; embedded files are expanded
// (a directory without its hidden files, unless all:); the test files, the
// testdata tree and the other files of the directory are apart; imports are
// those of the module; vendor, testdata and nested modules are not packages.
func TestReadGoModule(t *testing.T) {
	mod, err := ReadGoModule(testTree(t, moduleFiles()), "api")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for ip := range mod.Packages {
		got = append(got, strings.TrimPrefix(ip, "example.com/m/"))
	}
	sort.Strings(got)
	if strings.Join(got, " ") != "a b c d e onlytests" {
		t.Fatalf("packages: %v", got)
	}
	a := mod.Packages["example.com/m/a"]
	if names(a.LibFiles) != "a.go a_windows.go data/one.txt static/_x/y.txt static/z.txt" || names(a.TestFiles) != "a_test.go x_test.go" ||
		names(a.OtherFiles) != "a.lock" || a.Testdata == "" || !a.HasTests {
		t.Fatalf("a: lib %q test %q other %q testdata %q", names(a.LibFiles), names(a.TestFiles), names(a.OtherFiles), a.Testdata)
	}
	if strings.Join(a.Imports, " ") != "example.com/m/b" || strings.Join(a.TestImports, " ") != "example.com/m/c example.com/m/d" {
		t.Fatalf("a imports %v, test imports %v", a.Imports, a.TestImports)
	}
	if only := mod.Packages["example.com/m/onlytests"]; !only.HasTests || len(only.LibFiles) != 0 {
		t.Fatalf("a package of tests only: %+v", only)
	}
}

// A key changes with what its task reads and with nothing else.
func TestKeysFollowContent(t *testing.T) {
	keys := func(change func(map[string]string)) map[string]string {
		t.Helper()
		files := moduleFiles()
		if change != nil {
			change(files)
		}
		mod, err := ReadGoModule(testTree(t, files), "api")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for ip, p := range mod.Packages {
			name := strings.TrimPrefix(ip, "example.com/m/")
			out["L:"+name] = p.Lib
			if p.HasTests {
				parts, err := mod.TestKeyParts(ip)
				if err != nil {
					t.Fatal(err)
				}
				out["T:"+name] = Hash(parts)
			}
		}
		return out
	}
	base := keys(nil)
	for _, tc := range []struct {
		name    string
		change  func(map[string]string)
		changed string // the keys that must differ, the rest must not
	}{
		{"a file of an imported package", func(f map[string]string) { f["api/b/b.go"] += "// x\n" }, "L:a L:b T:a"},
		{"a file of a package only a test imports", func(f map[string]string) { f["api/c/c.go"] += "// x\n" }, "L:c T:a"},
		{"a test file", func(f map[string]string) { f["api/a/a_test.go"] += "// x\n" }, "T:a"},
		{"testdata", func(f map[string]string) { f["api/a/testdata/g.json"] = "[]" }, "T:a"},
		{"another file of the directory", func(f map[string]string) { f["api/a/a.lock"] = "lock-2" }, "T:a"},
		{"an embedded file", func(f map[string]string) { f["api/a/data/one.txt"] = "2" }, "L:a T:a"},
		{"a file a build constraint excludes", func(f map[string]string) { f["api/a/a_windows.go"] += "// x\n" }, "L:a T:a"},
		{"go.sum", func(f map[string]string) { f["api/go.sum"] = "sum-2\n" }, "T:a T:e T:onlytests"},
		{"a file outside the module", func(f map[string]string) { f["docs/readme.md"] = "bye" }, ""},
		{"a file the embed pattern does not match", func(f map[string]string) { f["api/a/data/skip.md"] = "yes" }, ""},
	} {
		got := keys(tc.change)
		var changed []string
		for k, v := range base {
			if got[k] != v {
				changed = append(changed, k)
			}
		}
		sort.Strings(changed)
		if strings.Join(changed, " ") != tc.changed {
			t.Errorf("%s: changed %v, want %q", tc.name, changed, tc.changed)
		}
	}
}

const manifestDoc = `version: 1
pins:
  ui: {commit: "1111111111111111111111111111111111111111", advance: manual}
modules:
  - name: api
    kind: go
    root: api
    image: go@sha256:aa
    template: run-db
    needs: ["ui:tasks/run-db.yaml"]
    tasks: {vet: {per: module}, test: {per: package}}
    shards: {api/a: 8}
    inputs:
      api/e: ["."]
  - name: console
    kind: node
    root: console
    needs: ["ui:."]
    tasks: {test: {shards: 4}, build: {}}
  - name: repo
    root: .
    tasks: {check: {needs: ["docs"]}}
`

func planFiles() (map[string]string, map[string]string) {
	files := moduleFiles()
	files[ManifestPath] = manifestDoc
	files["console/src/app.ts"] = "app"
	return files, map[string]string{"tasks/run-db.yaml": "db-1", "lib/x.ts": "x"}
}

func planKeys(t *testing.T, files, ui map[string]string) map[string]string {
	t.Helper()
	plan, err := Build("r", strings.Repeat("c", 40), testTree(t, files), Source{Pinned: func(repo, commit string) (*Tree, error) {
		if repo != "ui" || commit != strings.Repeat("1", 40) {
			return nil, fmt.Errorf("unexpected pin %s@%s", repo, commit)
		}
		return testTree(t, ui), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, u := range plan.Units {
		if u.Inputs["v"] != 2 || u.Inputs["unit"] != u.Unit || u.Inputs["task"] != u.Task || Hash(u.Inputs) != u.Key {
			t.Fatalf("unit %s %s: the key is not the hash of its inputs", u.Unit, u.Task)
		}
		out[u.Unit+" "+u.Task] = u.Key
		if u.Shards > 0 {
			out[u.Unit+" "+u.Task+" shards"] = fmt.Sprint(u.Shards)
		}
	}
	return out
}

// The plan: one test task a package with tests, one vet for the module, the
// console's tasks, the repository's check; and each key follows what the
// manifest says the task reads: "." everything, a pinned path only that path.
func TestBuildPlan(t *testing.T) {
	files, ui := planFiles()
	base := planKeys(t, files, ui)
	var units []string
	for k := range base {
		units = append(units, k)
	}
	sort.Strings(units)
	want := "api vet|api/a test|api/a test shards|api/e test|api/onlytests test|console build|console test|console test shards|repo check"
	if strings.Join(units, "|") != want || base["api/a test shards"] != "8" || base["console test shards"] != "4" {
		t.Fatalf("units: %s", strings.Join(units, "|"))
	}
	for _, tc := range []struct {
		name    string
		change  func(files, ui map[string]string)
		changed string
	}{
		{"a console file", func(f, _ map[string]string) { f["console/src/app.ts"] = "app2" }, "api/e test|console build|console test|repo check"},
		{"a doc", func(f, _ map[string]string) { f["docs/readme.md"] = "bye" }, "api/e test|repo check"},
		{"the pinned task template", func(_, u map[string]string) { u["tasks/run-db.yaml"] = "db-2" }, "api vet|api/a test|api/e test|api/onlytests test|console build|console test"},
		{"another file of the pinned repository", func(_, u map[string]string) { u["lib/x.ts"] = "x2" }, "api/e test|console build|console test"},
		{"a package's test file", func(f, _ map[string]string) { f["api/a/x_test.go"] += "// x\n" }, "api/a test|api/e test|repo check"},
		// The node tasks' own scripts are one line: what they run is unit-node.
		{"the script every node task runs", func(f, _ map[string]string) { f["scripts/ci/unit-node"] = "node-2" }, "api/e test|console build|console test|repo check"},
		{"a script no task of this plan runs", func(f, _ map[string]string) { f["scripts/ci/other"] = "other" }, "api/e test|repo check"},
	} {
		f, u := planFiles()
		tc.change(f, u)
		got := planKeys(t, f, u)
		var changed []string
		for k, v := range base {
			if got[k] != v {
				changed = append(changed, k)
			}
		}
		sort.Strings(changed)
		if strings.Join(changed, "|") != tc.changed {
			t.Errorf("%s: changed %s, want %s", tc.name, strings.Join(changed, "|"), tc.changed)
		}
	}
}

// A manifest that does not say what it means is refused, not completed.
func TestManifestRefusals(t *testing.T) {
	for name, doc := range map[string]string{
		"an unknown field":           strings.Replace(manifestDoc, "version: 1", "version: 1\nextra: 1", 1),
		"another version":            strings.Replace(manifestDoc, "version: 1", "version: 2", 1),
		"a pin that is not a commit": strings.Replace(manifestDoc, strings.Repeat("1", 40), "main", 1),
		// YAML reads forty digits as a number: an unquoted commit of digits only is refused, not guessed.
		"a commit of digits unquoted":  strings.Replace(manifestDoc, `"`+strings.Repeat("1", 40)+`"`, strings.Repeat("1", 40), 1),
		"an input of an unpinned repo": strings.Replace(manifestDoc, `needs: ["ui:."]`, `needs: ["other:."]`, 1),
		"a task with another name":     strings.Replace(manifestDoc, "build: {}", "deploy: {}", 1),
		"a go task without per":        strings.Replace(manifestDoc, "vet: {per: module}", "vet: {}", 1),
	} {
		if _, err := ParseManifest([]byte(doc)); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
	files, ui := planFiles()
	files[ManifestPath] = strings.Replace(manifestDoc, "api/e: [\".\"]", "api/b: [\".\"]", 1) // b has no tests
	if _, err := Build("r", strings.Repeat("c", 40), testTree(t, files), Source{Pinned: func(string, string) (*Tree, error) { return testTree(t, ui), nil }}); err == nil {
		t.Error("inputs declared for a package without tests were taken")
	}
	files[ManifestPath] = strings.Replace(manifestDoc, `needs: ["docs"]`, `needs: ["nowhere"]`, 1)
	if _, err := Build("r", strings.Repeat("c", 40), testTree(t, files), Source{Pinned: func(string, string) (*Tree, error) { return testTree(t, ui), nil }}); err == nil {
		t.Error("an input that names nothing was taken")
	}
}
