package testscope

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ConsoleResult contains repository-relative, space-separated file lists.
type ConsoleResult struct {
	Scope, Related, Tests, Lint string
	Build                       bool
	Why                         string
}

func ConsoleFull(why string) ConsoleResult {
	return ConsoleResult{Scope: "full", Build: true, Why: strings.Join(strings.Fields(why), " ")}
}
func (r ConsoleResult) Write(w io.Writer) {
	fmt.Fprintf(w, "console_scope=%s\nconsole_related=%s\nconsole_tests=%s\nconsole_lint=%s\nconsole_build=%t\nwhy=%s\n", r.Scope, r.Related, r.Tests, r.Lint, r.Build, r.Why)
}

// SelectConsole conservatively selects Console work, including tests reading API files.
func SelectConsole(repoRoot string, changes []Change) (result ConsoleResult) {
	defer func() {
		if recover() != nil {
			result = ConsoleFull("selector panic")
		}
	}()
	related, tests, lint := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var outside []string
	build := false
	for _, c := range changes {
		// Validate callers' changes with the same parser used for git input.
		if _, err := ParseChanges(strings.NewReader(c.Status + "\t" + strings.Join(c.Paths, "\t"))); err != nil {
			return ConsoleFull("invalid changes")
		}
		for _, p := range c.Paths {
			if strings.ContainsAny(p, " \t\r\n") {
				return ConsoleFull("path requires quoting")
			}
			base := filepath.Base(p)
			if p == "scripts/ci/test_console_scoped.sh" || p == "api/internal/testscope/console.go" || p == "console/package.json" || p == "console/package-lock.json" || strings.HasPrefix(p, "console/src/test/") || strings.HasPrefix(p, "console/scripts/") || (filepath.Dir(p) == "console" && (strings.Contains(base, ".config.") || (strings.HasPrefix(base, "tsconfig") && strings.HasSuffix(base, ".json")))) {
				return ConsoleFull("Console configuration or selector changed")
			}
			if strings.HasPrefix(p, "console/") {
				build = true
				if strings.HasPrefix(p, "console/src/") {
					related[p] = true
					if strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx") {
						if _, err := os.Stat(filepath.Join(repoRoot, p)); err == nil {
							lint[p] = true
						} else if !os.IsNotExist(err) {
							return ConsoleFull("source stat failed")
						}
					}
				}
			} else {
				outside = append(outside, p)
			}
		}
	}
	err := filepath.WalkDir(filepath.Join(repoRoot, "console/src"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.Contains(rel, "/__tests__/") && !strings.HasSuffix(rel, ".test.ts") && !strings.HasSuffix(rel, ".test.tsx") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, p := range outside {
			local, err := filepath.Rel(filepath.Dir(path), filepath.Join(repoRoot, p))
			if err != nil {
				return err
			}
			for _, needle := range []string{p, filepath.ToSlash(local), filepath.Base(p)} {
				if strings.Contains(string(b), needle) {
					tests[rel] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return ConsoleFull("Console test scan failed")
	}
	join := func(m map[string]bool) string {
		var a []string
		for p := range m {
			if strings.ContainsAny(p, " \t\r\n") {
				panic("unrepresentable path")
			}
			a = append(a, p)
		}
		sort.Strings(a)
		return strings.Join(a, " ")
	}
	scope := "incremental"
	why := "Console changes and external file readers"
	if !build && len(tests) == 0 {
		scope = "none"
		why = "no Console changes or test readers"
	}
	return ConsoleResult{scope, join(related), join(tests), join(lint), build, why}
}

// SelectConsoleChanges reads git name-status input, failing closed on malformed input.
func SelectConsoleChanges(root string, r io.Reader) ConsoleResult {
	changes, err := ParseChanges(r)
	if err != nil {
		return ConsoleFull("change list error: " + err.Error())
	}
	return SelectConsole(root, changes)
}

// SelectConsoleCommits applies the Go selector's history checks before selection.
func SelectConsoleCommits(root, base, head string) ConsoleResult {
	if _, err := command(root, "git", "cat-file", "-e", base+"^{commit}"); err != nil {
		return ConsoleFull("unreadable base")
	}
	if _, err := command(root, "git", "merge-base", "--is-ancestor", base, head); err != nil {
		return ConsoleFull("base is not an ancestor of head")
	}
	b, err := command(root, "git", "diff", "--name-status", "-M", base, head, "--")
	if err != nil {
		return ConsoleFull("git diff error")
	}
	return SelectConsoleChanges(root, strings.NewReader(string(b)))
}
