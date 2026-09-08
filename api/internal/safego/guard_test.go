package safego_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var bareGo = regexp.MustCompile(`^go [a-zA-Z_][a-zA-Z0-9_.]*\(`)

// Every goroutine in platform-api must be launched through safego or carry an
// inline `defer safego.Recover(...)`.
//
// chi's middleware.Recoverer guards only a request handler's own stack, so an
// unguarded goroutine takes the whole process with it — and this process runs
// Mission Control, Launch Desk, Build Desk, Plugin and Engineer together, so a
// panic in one work surface blinds the other four. This test is what keeps that
// from creeping back in one `go func` at a time.
func TestNoUnguardedGoroutines(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/safego/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "go func") && !bareGo.MatchString(trimmed) {
				continue
			}
			// safego.Go(...) is guarded by construction.
			if strings.Contains(trimmed, "safego.") {
				continue
			}
			// The guard must be among the first statements of the body, but a
			// comment explaining why is normal and must not read as a miss.
			if guardedWithin(lines[i+1:], 3) {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel+":"+itoa(i+1)+": "+trimmed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("unguarded goroutine(s) — launch via safego.Go or add `defer safego.Recover(name)`:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// guardedWithin reports whether one of the first maxStatements statements of a
// goroutine body registers safego.Recover, skipping blank lines and comments.
func guardedWithin(body []string, maxStatements int) bool {
	seen := 0
	for _, line := range body {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") {
			continue
		}
		if strings.Contains(t, "safego.Recover") {
			return true
		}
		seen++
		if seen >= maxStatements {
			return false
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
