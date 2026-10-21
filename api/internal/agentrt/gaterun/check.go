// Package gaterun runs the gate tests of the architecture table (TD-397,
// CUT-td397-gate-runs-2026-10-20.md): a gate is a few named tests of one
// package, run with `go test -json` on the commit an environment runs, and
// it passes only when every one of them ran and passed. A test that did not
// run (no match, a TestMain that exits early), that skipped (itself or any
// of its subtests), or that failed, and a package that does not build or
// fails, fail the gate: a gate never passes on tests that were not run, and
// a test database the tests need is required (AGENTRT_TEST_REQUIRED=1), so
// a missing one is a failure, never a skip.
//
// The package also holds what a gate's spec may be (spec.go) and the gates
// of each row (catalog.go).
package gaterun

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Outcome is how one named test of a gate ended.
type Outcome struct {
	Test   string
	Result string // pass, fail, skip, missing
	Why    string // for anything but pass
}

// event is one line of `go test -json` (cmd/test2json).
type event struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

// Report is what a gate run found: each named test's outcome, the
// package's own result, and the output of the tests that did not pass.
type Report struct {
	Outcomes []Outcome
	Package  string // pass, fail, or "" when the stream never said
	Failed   map[string][]string
	Bad      int // lines that are not test2json events
}

// Pass is whether the gate passed: every named test passed, the package
// passed, and every line of the stream was an event (review td397-3 B-b: a
// broken or cut line is not trusted to be harmless).
func (r Report) Pass() bool {
	if r.Package != "pass" || len(r.Outcomes) == 0 || r.Bad > 0 {
		return false
	}
	for _, o := range r.Outcomes {
		if o.Result != "pass" {
			return false
		}
	}
	return true
}

// Check reads a `go test -json` stream of one package and reports the
// outcome of each named test. Output lines that are not events are counted,
// not trusted.
func Check(stream io.Reader, tests []string) (Report, error) {
	named := map[string]bool{}
	for _, t := range tests {
		named[t] = true
	}
	ran, ended := map[string]bool{}, map[string]string{}
	skipped := map[string]string{} // a named test whose subtest skipped: the subtest
	subFailed := map[string]string{}
	output := map[string][]string{}
	r := Report{Failed: map[string][]string{}}
	sc := bufio.NewScanner(stream)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var e event
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil || e.Action == "" {
			r.Bad++
			continue
		}
		top, _, sub := strings.Cut(e.Test, "/")
		switch {
		case e.Test == "" && (e.Action == "pass" || e.Action == "fail"):
			r.Package = e.Action
		case e.Test == "" && e.Action == "skip":
			// A package with no test files, or every test filtered out.
			r.Package = "skip"
		case !named[top]:
		case e.Action == "output":
			output[top] = append(output[top], strings.TrimRight(e.Output, "\n"))
		case e.Action == "run" && !sub:
			ran[top] = true
		case e.Action == "skip" && sub:
			if _, ok := skipped[top]; !ok {
				skipped[top] = e.Test
			}
		case e.Action == "fail" && sub:
			if _, ok := subFailed[top]; !ok {
				subFailed[top] = e.Test
			}
		case (e.Action == "pass" || e.Action == "fail" || e.Action == "skip") && !sub:
			ended[top] = e.Action
		}
	}
	if err := sc.Err(); err != nil {
		return r, err
	}
	for _, t := range tests {
		o := Outcome{Test: t}
		switch {
		case !ran[t]:
			o.Result, o.Why = "missing", "it did not run"
		case ended[t] == "skip":
			o.Result, o.Why = "skip", "it skipped"
		case ended[t] == "fail":
			o.Result, o.Why = "fail", "it failed"
		case ended[t] == "":
			o.Result, o.Why = "fail", "it did not end"
		case subFailed[t] != "":
			o.Result, o.Why = "fail", "its subtest "+subFailed[t]+" failed"
		case skipped[t] != "":
			o.Result, o.Why = "skip", "its subtest "+skipped[t]+" skipped"
		default:
			o.Result = "pass"
		}
		if o.Result != "pass" {
			r.Failed[t] = output[t]
		}
		r.Outcomes = append(r.Outcomes, o)
	}
	return r, nil
}

// Summary is the report as lines, the failed tests' output first and each
// named test's outcome last; Verdict is the line after them.
func (r Report) Summary(tail int) []string {
	var out []string
	names := make([]string, 0, len(r.Failed))
	for t := range r.Failed {
		names = append(names, t)
	}
	sort.Strings(names)
	for _, t := range names {
		lines := r.Failed[t]
		if len(lines) > tail {
			lines = lines[len(lines)-tail:]
		}
		out = append(out, "--- output of "+t+" ---")
		out = append(out, lines...)
	}
	for _, o := range r.Outcomes {
		if o.Result == "pass" {
			out = append(out, "gate: PASS "+o.Test)
		} else {
			out = append(out, fmt.Sprintf("gate: %s %s: %s", strings.ToUpper(o.Result), o.Test, o.Why))
		}
	}
	return out
}

// VerdictMax is the most bytes of a gate's last line, which a verify.json's
// tail always keeps (review td397-3 B-c).
const VerdictMax = 300

// VerdictPrefix starts a gate's last line; what reads a gate's result reads
// that line only.
const VerdictPrefix = "gate verdict: "

// Verdict is the gate's last line: PASS with how many tests passed, or FAIL
// with the first test that did not pass and why (or why the run failed when
// every test passed: the package, a line that is not an event, go test's
// exit), at most VerdictMax bytes.
func (r Report) Verdict(pkg string, runErr error) string {
	line := ""
	switch first := r.firstFailure(); {
	case first != nil:
		line = fmt.Sprintf("FAIL %s: %s %s", pkg, first.Test, first.Why)
	case r.Package != "pass":
		result := r.Package
		if result == "" {
			result = "no result"
		}
		line = fmt.Sprintf("FAIL %s: the package's result is %s", pkg, result)
	case r.Bad > 0:
		line = fmt.Sprintf("FAIL %s: %d lines of go test's output are not test events", pkg, r.Bad)
	case runErr != nil:
		line = fmt.Sprintf("FAIL %s: go test: %v", pkg, runErr)
	case len(r.Outcomes) == 0:
		line = fmt.Sprintf("FAIL %s: no test was named", pkg)
	default:
		line = fmt.Sprintf("PASS %s: %d tests ran and passed", pkg, len(r.Outcomes))
	}
	line = VerdictPrefix + strings.ReplaceAll(line, "\n", " ")
	if len(line) > VerdictMax {
		line = line[:VerdictMax]
	}
	return line
}

func (r Report) firstFailure() *Outcome {
	for i := range r.Outcomes {
		if r.Outcomes[i].Result != "pass" {
			return &r.Outcomes[i]
		}
	}
	return nil
}
