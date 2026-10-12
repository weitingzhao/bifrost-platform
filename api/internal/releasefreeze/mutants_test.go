package releasefreeze

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	{"fifth", "lang.go", "\tif !membersOK(seen, dups) {\n\t\treturn i, false\n\t}\n\tif i >= len(b) || b[i] != '}' { // top ends after four members", "\tif i < len(b) && b[i] == ',' {\n\t\ti++\n\t\ti = skipW(b, i)\n\t\tvar ok5 bool\n\t\ti, ok5 = expectQuoted(b, i, \"z\")\n\t\tif ok5 {\n\t\t\ti = skipW(b, i)\n\t\t\tif i < len(b) && b[i] == ':' {\n\t\t\t\ti++\n\t\t\t\ti = skipW(b, i)\n\t\t\t\ti, _ = expectQuoted(b, i, \"\")\n\t\t\t\ti = skipW(b, i)\n\t\t\t}\n\t\t}\n\t}\n\tif !membersOK(seen, dups) {\n\t\treturn i, false\n\t}\n\tif i >= len(b) || b[i] != '}' { // top ends after four members", []string{"TestHandVectors"}},
	{"repeated-member", "lang.go", "if dups > 0 || seen != [4]bool{true, true, true, true} {", "if false && (dups > 0 || seen != [4]bool{true, true, true, true}) {", []string{"TestHandVectors"}},
	{"repeated-resourceVersion", "lang.go", "\tif j, ok := metaRC(b, start); ok {\n\t\treturn j, true\n\t}\n\treturn i, false\n}", "\tif j, ok := metaRC(b, start); ok {\n\t\treturn j, true\n\t}\n\tif j, ok := parseR(b, skipW(b, start)); ok {\n\t\tj = skipW(b, j)\n\t\tif j < len(b) && b[j] == ',' {\n\t\t\tif k, ok := parseR(b, skipW(b, j+1)); ok {\n\t\t\t\tk = skipW(b, k)\n\t\t\t\tif k < len(b) && b[k] == '}' {\n\t\t\t\t\treturn k + 1, true\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n\treturn i, false\n}", []string{"TestHandVectors"}},
	{"continue-nonempty", "lang.go", `func continueEmpty(s string) bool { return s == "" }`, `func continueEmpty(s string) bool { return true }`, []string{"TestHandVectors"}},
	{"digits-33", "lang.go", "maxDigits = 32", "maxDigits = 33", []string{"TestHandVectors"}},
	{"digits-0", "lang.go", "minDigits = 1", "minDigits = 0", []string{"TestHandVectors"}},
	{"whitespace", "lang.go", "return c == 0x20 || c == 0x09 || c == 0x0A || c == 0x0D", "return c == 0x20 || c == 0x09 || c == 0x0A || c == 0x0D || c == 'A'", []string{"TestGenerated"}},
	{"bytes-after-end", "lang.go", "func atEnd(n, i int) bool { return i == n }", "func atEnd(n, i int) bool { return true }", []string{"TestHandVectors"}},
	{"follow-redirect", "read.go", "\t\tif head.status >= 300 && head.status < 400 { // rule: a redirect is not followed\n\t\t\treturn notClear(\"HTTP \" + strconv.Itoa(head.status)), nil\n\t\t}", "\t\tif head.status >= 300 && head.status < 400 { // rule: a redirect is not followed\n\t\t\tpath := \"\"\n\t\t\tif vs := head.values(\"location\"); len(vs) > 0 {\n\t\t\t\tpath = vs[0]\n\t\t\t}\n\t\t\tif !strings.HasPrefix(path, \"/\") {\n\t\t\t\treturn notClear(\"HTTP \" + strconv.Itoa(head.status)), nil\n\t\t\t}\n\t\t\ttr := http1Transport(pool)\n\t\t\taddr := g.Addr\n\t\t\tif addr == \"\" {\n\t\t\t\taddr = OriginHost + \":443\"\n\t\t\t}\n\t\t\tvar c2 net.Conn\n\t\t\tvar err2 error\n\t\t\tif g.Dial != nil {\n\t\t\t\tc2, err2 = g.Dial(ctx, \"tcp\", addr, tr.TLSClientConfig, deadline)\n\t\t\t} else {\n\t\t\t\tc2, err2 = defaultDial(ctx, addr, tr.TLSClientConfig, deadline)\n\t\t\t}\n\t\t\tif err2 != nil {\n\t\t\t\treturn Decision{}, err2\n\t\t\t}\n\t\t\tdefer c2.Close()\n\t\t\tarmDeadline(c2, deadline)\n\t\t\tif _, err := io.WriteString(c2, \"GET \"+path+\" HTTP/1.1\\r\\nHost: \"+OriginHost+\"\\r\\nAccept: application/json\\r\\nAccept-Encoding: identity\\r\\nAuthorization: Bearer \"+token+\"\\r\\nConnection: close\\r\\n\\r\\n\"); err != nil {\n\t\t\t\treturn Decision{}, err\n\t\t\t}\n\t\t\tbr2 := bufio.NewReader(c2)\n\t\t\th2, err := readHead(br2, c2, deadline)\n\t\t\tif err != nil {\n\t\t\t\treturn Decision{}, err\n\t\t\t}\n\t\t\tbody2, err := readBody(br2, c2, deadline, h2)\n\t\t\tif err != nil {\n\t\t\t\treturn Decision{}, err\n\t\t\t}\n\t\t\treturn DecideHTTP(h2.status, body2), nil\n\t\t}", []string{"TestReadDoesNotFollowARedirect"}},
	{"content-encoding", "read.go", "func encodingForbidden(present bool) bool { return present }", "func encodingForbidden(present bool) bool { return false }", []string{"TestReadRejectsContentEncoding"}},
	{"body-4097", "decision.go", "func bodyTooBig(n int) bool { return n > maxBody }", "func bodyTooBig(n int) bool { return false }", []string{"TestReadBodyLimit"}},
	{"truncated-length", "read.go", "func lengthComplete(got, want int) bool { return got == want }", "func lengthComplete(got, want int) bool { return true }", []string{"TestReadTruncatedContentLength"}},
	{"unterminated-chunked", "read.go", "func chunkFrameComplete(finalCRLF bool) bool { return finalCRLF }", "func chunkFrameComplete(finalCRLF bool) bool { return true }", []string{"TestReadChunked"}},
	{"skip-hostname", "read.go", "InsecureSkipVerify: false, // hostname and chain are verified", "InsecureSkipVerify: true, // hostname and chain are verified", []string{"TestReadWrongHostname"}},
	{"per-op-deadline", "read.go", "\t_ = conn.SetDeadline(deadline) // one total deadline\n}\n\n// finishedInTime is the second hold on the deadline: clear only when the response was complete before it.\nfunc finishedInTime(deadline time.Time) bool { return !time.Now().After(deadline) }", "\t_ = conn.SetDeadline(time.Now().Add(10 * time.Second)) // one total deadline\n}\n\n// finishedInTime is the second hold on the deadline: clear only when the response was complete before it.\nfunc finishedInTime(deadline time.Time) bool { return true }", []string{"TestReadSlowDripUsesOneDeadline", "TestHTTPTiming"}},
	{"second-deadline-hold", "read.go", "\t\tif got.Verdict == VerdictClear && !finishedInTime(deadline) {\n", "\t\tif false && got.Verdict == VerdictClear && !finishedInTime(deadline) {\n", []string{"TestSecondDeadlineHold"}},
	{"close-notify", "read.go", "\tdefer closeConn(conn)\n", "\tdefer conn.Close()\n", []string{"TestReadReturnsWithoutWaitingForCloseNotify"}},
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
	{"chunk-data-crlf", "httplang.go", "if _, err := io.ReadFull(br, crlf[:]); err != nil || crlf != [2]byte{'\\r', '\\n'} {", "if _, err := io.ReadFull(br, crlf[:]); err != nil {", []string{"TestHTTPVectors"}},
	{"status-first-byte", "httplang.go", "\tif len(line) < len(prefix)+3 || string(line[:len(prefix)]) != prefix {\n", "\tif len(line) < len(prefix)+3 || string(line[1:len(prefix)]) != prefix[1:] {\n", []string{"TestHTTPByteClasses"}},
}

// mutantEvidence names, for a planted violation of H, the response whose verdict has to flip: the failure must
// come from that case, not from a missing fixture or any other breakage.
var mutantEvidence = map[string]string{
	"line-1025":             "a field line of 1025 bytes",
	"fields-65":             "65 field lines",
	"interim-9":             " x9",
	"length-5-digits":       "content-length value b'00126'",
	"hex-5-digits":          "chunk size b'0007e'",
	"bare-lf":               "bare LF after the status line",
	"reason-without-sp":     "status line b'HTTP/1.1 200OK'",
	"field-text-wide":       "extra field b'X: \\x80'",
	"token-wide":            "extra field b'X Junk: 1'",
	"chunked-with-length":   "extra field, chunked b'Content-Length: 126'",
	"other-transfer-coding": "one transfer-encoding field b'Transfer-Encoding: notchunked'",
	"chunk-extension":       "chunk size b'7e;ext=1'",
	"trailer-framing-field": "chunk terminator b'0\\r\\nContent-Encoding",
	"length-not-digits":     "content-length value b'+126'",
	"field-without-colon":   "extra field b'X-Junk'",
	"chunk-data-crlf":       "two bytes that are not CRLF after the chunk data",
	"status-first-byte":     "a replace 0",
}

// evidenceHeld reports whether want names a vector, or a byte-class position ("<base> <mode> <index>").
func evidenceHeld(file httpVectorFile, want string) bool {
	for _, c := range file.Cases {
		if strings.Contains(c.Name, want) {
			return true
		}
	}
	parts := strings.Split(want, " ")
	if len(parts) != 3 {
		return false
	}
	index, err := strconv.Atoi(parts[2])
	if err != nil {
		return false
	}
	for _, base := range file.ByteClasses {
		if base.Name != parts[0] {
			continue
		}
		positions := base.Positions
		if parts[1] == "insert" {
			positions = base.InsertPositions
		} else if parts[1] != "replace" {
			return false
		}
		for _, k := range positions {
			if k == index {
				return true
			}
		}
	}
	return false
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
	vectors := loadHTTPVectorFile(t)
	for name, want := range mutantEvidence {
		if !seen[name] {
			t.Fatalf("evidence for an unknown mutant %s", name)
		}
		if !evidenceHeld(vectors, want) {
			t.Fatalf("evidence of %s names no vector and no position: %q", name, want)
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
	controlled := map[string]bool{}
	for _, m := range mutants {
		// The copy sits beside the package, at the same depth, so the tests find their sibling repository as they do in place.
		work := filepath.Join(filepath.Dir(dir), "_mutant_releasefreeze_"+m.name)
		if err := os.RemoveAll(work); err != nil {
			t.Fatal(err)
		}
		if err := copyPackage(dir, work); err != nil {
			t.Fatal(err)
		}
		// Negative control: the named tests pass on the copy before anything is planted. A copy that
		// fails by itself (a missing fixture, a broken build) proves nothing about the planted change.
		{
			pattern := "^(" + strings.Join(m.mustFail, "|") + ")$"
			if !controlled[pattern] {
				cmd := exec.Command("go", "test", "-count=1", "-timeout", "120s", "-run", pattern, "-v", "./internal/_mutant_releasefreeze_"+m.name)
				cmd.Dir = mod
				out, err := cmd.CombinedOutput()
				if err != nil || !strings.Contains(string(out), "--- PASS: ") {
					_ = os.RemoveAll(work)
					t.Fatalf("negative control for %s: the named tests do not pass on an unmutated copy\n%s", m.name, out)
				}
				controlled[pattern] = true
			}
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
		cmd := exec.Command("go", "test", "-count=1", "-timeout", "120s", "-run", run, "./internal/_mutant_releasefreeze_"+m.name)
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
		if want := mutantEvidence[m.name]; want != "" && !strings.Contains(string(out), want) {
			t.Errorf("mutant %s failed a test, but not on the response it is about (%q)\n%s", m.name, want, out)
			fmt.Printf("mutant %s failed without its evidence\n", m.name)
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
		if e.IsDir() && e.Name() == "testdata" {
			// The tests read their fixtures from here; a copy without them fails for the wrong reason.
			files, err := os.ReadDir(filepath.Join(src, "testdata"))
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(dst, "testdata"), 0o755); err != nil {
				return err
			}
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				raw, err := os.ReadFile(filepath.Join(src, "testdata", f.Name()))
				if err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(dst, "testdata", f.Name()), raw, 0o644); err != nil {
					return err
				}
			}
			continue
		}
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
