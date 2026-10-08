// Package tradevocab holds the ratchet on Trade vocabulary compiled into
// platform-api (ratchet platform-trade-vocab, TD-231).
package tradevocab

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// tradeVocab matches the application's symbols, Redis keys, stream names and
// namespaces. The platform is meant to be pointed at another application by
// configuration; each literal here is a place where it cannot be.
var tradeVocab = regexp.MustCompile(`NVDA|"ib:|ws_ib_|auto_status|"bifrost-(prod|stg|dev)"`)

// budget is the number of matching lines each non-test file may still have.
// It may only fall: a file over budget or missing from the list fails, and a
// file under budget fails until its number is lowered. Namespaces belong in
// environments.yaml (config.AppEnvs), contracts in the plugin's ConfigMap.
var budget = map[string]int{
	"delivery/supply_chain.go":     4,
	"ibgateway/autorepair.go":      1,
	"ibgateway/config.go":          1,
	"ibgateway/operator_cmd.go":    4,
	"ibgateway/plugin_health.go":   1,
	"ibgateway/sample_contract.go": 1,
	"ibgateway/service.go":         7,
	"patrol/autopilot.go":          3,
	"placement/evaluate.go":        1,
	"satellite/service.go":         1,
	"satellite/types.go":           1,
	"telemetry/queries.go":         4,
}

func TestTradeVocabularyOnlyShrinks(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]int{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if tradeVocab.MatchString(line) {
				n++
			}
		}
		if n > 0 {
			rel, _ := filepath.Rel(root, path)
			found[filepath.ToSlash(rel)] = n
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, 0, len(found)+len(budget))
	for f := range found {
		files = append(files, f)
	}
	for f := range budget {
		if _, ok := found[f]; !ok {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	for _, f := range files {
		got, allowed := found[f], budget[f]
		switch {
		case got > allowed:
			t.Errorf("%s: %d lines of Trade vocabulary, budget %d — read it from configuration instead", f, got, allowed)
		case got < allowed:
			t.Errorf("%s: %d lines, budget %d — lower the budget (it only shrinks)", f, got, allowed)
		}
	}
}
