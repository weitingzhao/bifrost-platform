//bifrost:owner verification

package server

import (
	"encoding/json"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func frozenShadowRecord(line []any) (status, detail string) {
	doc := map[string]any{}
	for i := 0; i+1 < len(line); i += 2 {
		if key, ok := line[i].(string); ok {
			doc[key] = line[i+1]
		}
	}
	status = "ok"
	if missed, ok := doc["missed"].([]string); ok && len(missed) > 0 {
		status = "missed"
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return status, fmt.Sprint(line...)
	}
	return status, string(raw)
}

func frozenShadowLine(p seenfacts.PipelineRun, commit string, scope unitplan.Scope) []any {
	same := map[string]bool{} // unit -> its keys are the base's
	run, byModule := 0, []string{}
	for _, m := range scope.Modules {
		for _, unit := range m.RunUnits {
			same[unit] = false
		}
		for _, unit := range m.SameUnits {
			same[unit] = true
		}
		run += len(m.RunUnits)
		byModule = append(byModule, fmt.Sprintf("%s=%d/%d", m.Module, len(m.RunUnits), len(m.RunUnits)+len(m.SameUnits)))
	}
	missed, unmatched := map[string]bool{}, 0
	for _, test := range p.FailedTests {
		pkg := frozenTestPackage(test)
		best, fits := 0, false
		var skipped []string
		for unit, isSame := range same {
			n := frozenUnitMatch(pkg, unit)
			if n == 0 || n < best {
				continue
			}
			if n > best {
				best, fits, skipped = n, false, nil
			}
			fits = true
			if isSame {
				skipped = append(skipped, unit)
			}
		}
		if !fits {
			unmatched++
		}
		// Two units that fit a package equally: one the scope leaves out is
		// named, since a miss is what must not go unseen.
		for _, unit := range skipped {
			missed[unit] = true
		}
	}
	names := make([]string, 0, len(missed))
	for unit := range missed {
		names = append(names, unit)
	}
	sort.Strings(names)
	return []any{"run", p.Run, "revision", commit, "base", scope.Base, "verdict", p.Result, "run_scope", p.Scope,
		"would_run", run, "total", len(same), "by_module", strings.Join(byModule, " "), "reason", scope.Note,
		"missed", names, "failed_unnamed", p.Result == "Failed" && len(p.FailedTests) == 0, "unmatched", unmatched}
}

// testPackage is the package path of a failed test's name
// ("<package path>.<TestName>[/subtest]"): what stands before the test
// function. A name with no test function in it is no package's.
func frozenTestPackage(test string) string {
	at := -1
	for _, kind := range []string{".Test", ".Example", ".Fuzz", ".Benchmark"} {
		if i := strings.Index(test, kind); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	if at < 0 {
		return ""
	}
	return test[:at]
}

// unitMatch is how many of a unit's last path elements a package path ends
// with, 0 when not even the last. A unit is named by its directory in the
// repository and a package by its path in its module, whose root the plan
// does not say: the unit that fits with the most elements is the package's.
func frozenUnitMatch(pkg, unit string) int {
	if pkg == "" {
		return 0
	}
	parts := strings.Split(unit, "/")
	for n := len(parts); n > 0; n-- {
		tail := strings.Join(parts[len(parts)-n:], "/")
		if pkg == tail || strings.HasSuffix(pkg, "/"+tail) {
			return n
		}
	}
	return 0
}

func TestScopeShadowFrozenProjection(t *testing.T) {
	t.Parallel()
	for n := 0; n < 9; n++ {
		for _, result := range []string{"", "Succeeded", "Failed"} {
			p := seenfacts.PipelineRun{Run: "ci-<>&", Result: result, Scope: "scope"}
			scope := unitplan.Scope{Base: "base", Note: "reason"}
			for i := 0; i < n; i++ {
				m := unitplan.ModuleScope{Module: fmt.Sprint(i), RunUnits: []string{fmt.Sprintf("svc/%d/a", i)}, SameUnits: []string{fmt.Sprintf("pkg/%d/a", i)}}
				scope.Modules = append(scope.Modules, m)
				p.FailedTests = append(p.FailedTests, fmt.Sprintf("example/%d/a.TestX/sub.TestY", i), "build failed", "example/b.FuzzX")
			}
			want := frozenShadowLine(p, "commit", scope)
			got := shadowLine(p, "commit", scope)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("%#v != %#v", want, got)
			}
			ws, wd := frozenShadowRecord(want)
			gs, gd := shadowRecord(got)
			if ws != gs || wd != gd {
				t.Fatalf("%s %s != %s %s", ws, wd, gs, gd)
			}
		}
	}
	for _, line := range [][]any{nil, {1, "skip", "missed", []string{"a"}, "odd"}, {"missed", "wrong-type"}, {"bad", func() {}}} {
		ws, wd := frozenShadowRecord(line)
		gs, gd := shadowRecord(line)
		if ws != gs || wd != gd {
			t.Fatalf("record parity %s %s != %s %s", ws, wd, gs, gd)
		}
	}
}
