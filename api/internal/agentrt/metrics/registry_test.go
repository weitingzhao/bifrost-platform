package metrics

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Keep historical versions when adding a new version to this lock.
type definitionLock map[string]map[int]string

func checkLock(defs []Definition, lock definitionLock) string {
	current := map[string]bool{}
	for _, d := range defs {
		current[d.ID] = true
		versions, ok := lock[d.ID]
		if !ok {
			return d.ID + ": update the lock for the new definition"
		}
		for version := range versions {
			if version > d.Version {
				return d.ID + ": versions must never go down against the lock"
			}
		}
		hash, ok := versions[d.Version]
		if !ok {
			return d.ID + ": update the lock for the new version"
		}
		if hash != d.SHA256 {
			return d.ID + ": definition changed; raise the version and update the lock"
		}
	}
	for id := range lock {
		if !current[id] {
			return id + ": locked definition is missing"
		}
	}
	return ""
}

func TestDefinitionsLock(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("definitions.lock")
	if err != nil {
		t.Fatal(err)
	}
	var lock definitionLock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	if msg := checkLock(r.Definitions(), lock); msg != "" {
		t.Fatal(msg)
	}
	if len(r.Definitions()) != 10 {
		t.Fatalf("got %d definitions, want 10", len(r.Definitions()))
	}
}

func TestLockGuard(t *testing.T) {
	d := Definition{ID: "example", Version: 2, SHA256: "new"}
	tests := []struct {
		name string
		lock definitionLock
		want string
	}{
		{"changed", definitionLock{"example": {2: "old"}}, "raise the version and update the lock"},
		{"downgrade", definitionLock{"example": {3: "old"}}, "versions must never go down"},
		{"new version", definitionLock{"example": {1: "old", 2: "new"}}, ""},
		{"unlocked", definitionLock{"example": {1: "old"}}, "update the lock"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkLock([]Definition{d}, tt.lock)
			if (tt.want == "" && got != "") || !strings.Contains(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStrictParsing(t *testing.T) {
	source := string(definitionsYAML)
	tests := map[string]string{
		"unknown field":     strings.Replace(source, "  title:", "  typo:", 1),
		"unknown extractor": strings.Replace(source, "extractor: task_lead_time", "extractor: unknown", 1),
		"duplicate id":      strings.Replace(source, "id: owner_wait_share", "id: lead_time_s", 1),
		"zero version":      strings.Replace(source, "version: 1", "version: 0", 1),
		"unknown unit":      strings.Replace(source, "unit: seconds", "unit: minutes", 1),
		"unknown headline":  strings.Replace(source, "headline: p50", "headline: max", 1),
		"unknown window":    strings.Replace(source, "[hour, day, week, span]", "[month]", 1),
		"negative settle":   strings.Replace(source, "settle_s: 300", "settle_s: -1", 1),
		"empty":             "[]",
		"extra document":    source + "\n---\n[]\n",
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parse([]byte(data)); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
}

func TestDue(t *testing.T) {
	r := &Registry{definitions: []Definition{{ID: "a", Version: 2}, {ID: "b", Version: 1}}}
	have := map[DefinitionKey]bool{
		{ID: "a", Version: 1, Label: "day:1"}: true, // Old baselines must be recomputed.
		{ID: "a", Version: 2, Label: "day:2"}: true,
		{ID: "b", Version: 1, Label: "day:1"}: false,
	}
	want := []DefinitionKey{{"a", 2, "day:1"}, {"b", 1, "day:1"}, {"b", 1, "day:2"}}
	if got := r.Due(have, []string{"day:2", "day:1", "day:1"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if got := r.Due(nil, nil); len(got) != 0 {
		t.Fatalf("no labels: %#v", got)
	}
	if got := r.Due(nil, []string{"span:x"}); len(got) != 2 {
		t.Fatalf("nil have: %#v", got)
	}
}

func TestDefinitionsSortedAndIndependent(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	defs := r.Definitions()
	for i := 1; i < len(defs); i++ {
		if defs[i-1].ID >= defs[i].ID {
			t.Fatal("definitions not uniquely sorted")
		}
	}
	before := r.Definitions()
	defs[0].WindowKinds[0] = "changed"
	defs[0].Params["kind"] = "changed"
	if !reflect.DeepEqual(before, r.Definitions()) {
		t.Fatal("caller mutated registry")
	}
}

func TestCanonicalHash(t *testing.T) {
	a, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// YAML presentation and map key ordering must not affect the hash.
	changed := strings.Replace(string(definitionsYAML), `{"kind": "approval"}`, `{kind: approval}`, 1)
	changed = strings.Replace(changed, `{"start": "first_attempt_started", "end": "commit_running_on_prod", "also": ["mean", "p90"]}`, `{also: [mean, p90], end: commit_running_on_prod, start: first_attempt_started}`, 1)
	b, err := parse([]byte("# comment\n" + changed))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Definitions(), b.Definitions()) {
		t.Fatal("presentation changed canonical definitions")
	}
	changed = strings.Replace(string(definitionsYAML), "settle_s: 300", "settle_s: 301", 1)
	c, err := parse([]byte(changed))
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range a.Definitions() {
		if d.ID == "lead_time_s" && d.SHA256 == c.Definitions()[i].SHA256 {
			t.Fatal("semantic change did not change hash")
		}
	}
}
