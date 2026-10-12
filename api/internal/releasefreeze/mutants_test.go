package releasefreeze

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type mutant struct {
	name     string
	file     string
	old      string
	new      string
	mustFail []string
}

// mutants are planted violations. Each old string occurs once. mustFail names
// tests that have to fail once that string is replaced.
var mutants = []mutant{
	{"fifth", "lang.go", "func allowFifth() bool { return false }", "func allowFifth() bool { return true }", []string{"TestHandVectors"}},
	{"repeated-member", "lang.go", "if dups > 0 || seen != [4]bool{true, true, true, true} {", "if false && (dups > 0 || seen != [4]bool{true, true, true, true}) {", []string{"TestHandVectors"}},
	{"repeated-resourceVersion", "lang.go", "var metaExtras = []func([]byte, int) (int, bool){}", "var metaExtras = []func([]byte, int) (int, bool){metaRR}", []string{"TestHandVectors"}},
	{"continue-nonempty", "lang.go", `func continueEmpty(s string) bool { return s == "" }`, `func continueEmpty(s string) bool { return true }`, []string{"TestHandVectors"}},
	{"digits-33", "lang.go", "maxDigits = 32", "maxDigits = 33", []string{"TestHandVectors"}},
	{"digits-0", "lang.go", "minDigits = 1", "minDigits = 0", []string{"TestHandVectors"}},
	{"whitespace", "lang.go", "return c == 0x20 || c == 0x09 || c == 0x0A || c == 0x0D", "return c == 0x20 || c == 0x09 || c == 0x0A || c == 0x0D || c == 'A'", []string{"TestGenerated"}},
	{"bytes-after-end", "lang.go", "func atEnd(n, i int) bool { return i == n }", "func atEnd(n, i int) bool { return true }", []string{"TestHandVectors"}},
	{"follow-redirect", "read.go", "func followRedirect() bool { return false }", "func followRedirect() bool { return true }", []string{"TestReadDoesNotFollowARedirect"}},
	{"content-encoding", "read.go", "func encodingForbidden(present bool) bool { return present }", "func encodingForbidden(present bool) bool { return false }", []string{"TestReadRejectsContentEncoding"}},
	{"body-4097", "decision.go", "func bodyTooBig(n int) bool { return n > maxBody }", "func bodyTooBig(n int) bool { return false }", []string{"TestReadBodyLimit"}},
	{"truncated-length", "read.go", "func lengthComplete(got, want int) bool { return got == want }", "func lengthComplete(got, want int) bool { return true }", []string{"TestReadTruncatedContentLength"}},
	{"unterminated-chunked", "read.go", "func chunkFrameComplete(finalCRLF bool) bool { return finalCRLF }", "func chunkFrameComplete(finalCRLF bool) bool { return true }", []string{"TestReadChunked"}},
	{"skip-hostname", "read.go", "InsecureSkipVerify: false, // hostname and chain are verified", "InsecureSkipVerify: true, // hostname and chain are verified", []string{"TestReadWrongHostname"}},
	{"per-op-deadline", "read.go", "_ = conn.SetDeadline(deadline) // one total deadline", "_ = conn.SetDeadline(time.Now().Add(10 * time.Second)) // one total deadline", []string{"TestReadSlowDripUsesOneDeadline"}},
	{"status-404", "decision.go", "return false // 404 is not a list", "return true // 404 is not a list", []string{"TestStatusIsNotAList"}},
	{"status-403", "decision.go", "return false // 403 is not a list", "return true // 403 is not a list", []string{"TestStatusIsNotAList"}},
	{"status-500", "decision.go", "return false // 500 is not a list", "return true // 500 is not a list", []string{"TestStatusIsNotAList"}},
	{"fail-open", "decision.go", "func failClosed(token string) Decision {\n\treturn notClear(token)\n}", "func failClosed(token string) Decision {\n\treturn Decision{Verdict: VerdictClear, Lines: []string{lineClear}}\n}", []string{"TestFaultInjectionSeams"}},
	{"owner-ref", "model_engine_test.go", "refuseOwnerRef:        true, // rule: no owner reference", "refuseOwnerRef:        false, // rule: no owner reference", []string{"TestOwnerRefCreateIsRefused"}},
	{"delete-username", "model_engine_test.go", "ownerOnlyDelete:       true, // rule: delete username is the Owner", "ownerOnlyDelete:       false, // rule: delete username is the Owner", []string{"TestExhaustive"}},
	{"create-guard", "model_engine_test.go", "createNeedsTombstone:  true, // rule: tombstone before create", "createNeedsTombstone:  false, // rule: tombstone before create", []string{"TestExhaustive"}},
	{"sweep-admission", "model_engine_test.go", "sweepThroughAdmission: true, // rule: sweep uses admission", "sweepThroughAdmission: false, // rule: sweep uses admission", []string{"TestSweepGoesThroughAdmission"}},
	{"line-1025", "httplang.go", "maxLine      = 1024 // rule", "maxLine      = 1025 // rule", []string{"TestHTTPVectors"}},
	{"fields-65", "httplang.go", "maxFields    = 64   // rule", "maxFields    = 65   // rule", []string{"TestHTTPVectors"}},
	{"interim-9", "httplang.go", "maxInterim   = 8    // rule", "maxInterim   = 9    // rule", []string{"TestHTTPVectors"}},
	{"length-5-digits", "httplang.go", "maxLenDigits = 4    // rule", "maxLenDigits = 5    // rule", []string{"TestHTTPVectors"}},
	{"hex-5-digits", "httplang.go", "maxHexDigits = 4    // rule", "maxHexDigits = 5    // rule", []string{"TestHTTPVectors"}},
	{"bare-lf", "httplang.go", "if n < 2 || line[n-2] != '\\r' { // rule: a line ends with CRLF", "if n < 2 { // rule: a line ends with CRLF", []string{"TestHTTPVectors"}},
	{"reason-without-sp", "httplang.go", "if rest[0] != ' ' { // rule: SP between the status code and the reason", "if false { // rule: SP between the status code and the reason", []string{"TestHTTPVectors"}},
	{"field-text-wide", "httplang.go", "return c == 0x09 || (c >= 0x20 && c <= 0x7E) // rule: field text is HTAB or 0x20-0x7E", "return c == 0x09 || c >= 0x20 // rule: field text is HTAB or 0x20-0x7E", []string{"TestHTTPVectors"}},
	{"token-wide", "httplang.go", "'|', '~': // rule: the fifteen other token bytes", "'|', '~', ' ', '(': // rule: the fifteen other token bytes", []string{"TestHTTPVectors"}},
	{"chunked-with-length", "httplang.go", "if len(cl) != 0 || len(te) != 1 { // rule: chunked excludes content-length and a second transfer-encoding", "if len(te) != 1 { // rule: chunked excludes content-length and a second transfer-encoding", []string{"TestHTTPVectors"}},
	{"other-transfer-coding", "httplang.go", "if lowerASCII([]byte(te[0])) != \"chunked\" { // rule: the only transfer coding is chunked", "if false { // rule: the only transfer coding is chunked", []string{"TestHTTPVectors"}},
	{"chunk-extension", "httplang.go", "func chunkSizeText(line []byte) []byte { return line } // rule: no chunk extension", "func chunkSizeText(line []byte) []byte {\n\tfor i, c := range line {\n\t\tif c == ';' {\n\t\t\treturn line[:i]\n\t\t}\n\t}\n\treturn line\n} // rule: no chunk extension", []string{"TestHTTPVectors"}},
	{"trailer-framing-field", "httplang.go", "if f.name == \"content-encoding\" || f.name == \"content-length\" || f.name == \"transfer-encoding\" { // rule: no framing field in a trailer", "if f.name == \"\" { // rule: no framing field in a trailer", []string{"TestHTTPVectors"}},
	{"length-not-digits", "httplang.go", "\t\tif !isDigit(s[i]) {\n\t\t\treturn 0, false\n\t\t}\n\t\tn = n*10 + int(s[i]-'0')", "\t\tif !isDigit(s[i]) {\n\t\t\tcontinue\n\t\t}\n\t\tn = n*10 + int(s[i]-'0')", []string{"TestHTTPVectors"}},
	{"field-without-colon", "httplang.go", "\tif i == 0 || i >= len(line) || line[i] != ':' {\n\t\treturn \"\", \"\", false\n\t}", "\tif i == len(line) && i > 0 {\n\t\treturn lowerASCII(line), \"\", true\n\t}\n\tif i == 0 || i >= len(line) || line[i] != ':' {\n\t\treturn \"\", \"\", false\n\t}", []string{"TestHTTPVectors"}},
}

func TestMutantList(t *testing.T) {
	dir := packageDir(t)
	src := testSource(t, dir)
	if len(mutants) < 20 {
		t.Fatalf("mutants %d", len(mutants))
	}
	seen := map[string]bool{}
	for _, m := range mutants {
		if seen[m.name] {
			t.Fatalf("duplicate mutant %s", m.name)
		}
		seen[m.name] = true
		raw, err := os.ReadFile(filepath.Join(dir, m.file))
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(raw), m.old); n != 1 {
			t.Fatalf("%s old occurs %d times in %s", m.name, n, m.file)
		}
		if m.old == m.new || len(m.mustFail) == 0 {
			t.Fatalf("%s is empty", m.name)
		}
		for _, name := range m.mustFail {
			if !strings.Contains(src, "func "+name+"(") {
				t.Fatalf("%s names missing test %s", m.name, name)
			}
		}
	}
	t.Logf("mutants %d", len(mutants))
}

func TestMutantRuns(t *testing.T) {
	if os.Getenv("BIFROST_FREEZE_MUTANTS") != "1" {
		t.Skip("BIFROST_FREEZE_MUTANTS is not set")
	}
	dir := packageDir(t)
	mod := filepath.Dir(filepath.Dir(dir))
	for _, m := range mutants {
		work := filepath.Join(dir, "_mutant_"+m.name)
		if err := os.RemoveAll(work); err != nil {
			t.Fatal(err)
		}
		if err := copyPackage(dir, work); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(work, m.file)
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		next := strings.Replace(string(raw), m.old, m.new, 1)
		if next == string(raw) {
			t.Fatalf("%s did not replace", m.name)
		}
		if err := os.WriteFile(target, []byte(next), 0o644); err != nil {
			t.Fatal(err)
		}
		run := "^(" + strings.Join(m.mustFail, "|") + ")$"
		cmd := exec.Command("go", "test", "-count=1", "-timeout", "120s", "-run", run, "./internal/releasefreeze/_mutant_"+m.name)
		cmd.Dir = mod
		out, err := cmd.CombinedOutput()
		_ = os.RemoveAll(work)
		if err == nil {
			t.Errorf("mutant %s survived\n%s", m.name, out)
			fmt.Printf("mutant %s survived\n", m.name)
			continue
		}
		// Caught means a named test FAILED. A build failure is not a catch.
		if !strings.Contains(string(out), "--- FAIL: ") || strings.Contains(string(out), "[build failed]") || strings.Contains(string(out), "[setup failed]") {
			t.Errorf("mutant %s broke the build without failing a test\n%s", m.name, out)
			fmt.Printf("mutant %s broke the build without failing a test\n", m.name)
			continue
		}
		fmt.Printf("mutant %s killed\n", m.name)
		t.Logf("mutant %s killed", m.name)
	}
}

func packageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Dir(file)
}

func testSource(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
	}
	return b.String()
}

func copyPackage(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if e.Name() == "mutants_test.go" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if strings.HasPrefix(string(raw), "//go:build ignore\n") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}
