package store_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// corePlan is goodPlan whose first task is core work: it names the agentd
// file it changes; the second stays as it was.
var corePlan = strings.Replace(goodPlan, `"text": "Say which ref MainHead reads.",`,
	`"text": "In api/internal/agentd/executors.go, keep whole characters in oneLine.", "core": true,`, 1)

// W-31 2.4, the Owner's 「核心字段 A」 (10-19): a plan may mark a task core.
// true and false and leaving it out read; null and other types are read as
// true where the task can be core work, never silently as false; core is
// read as false, with a note, in a repository with no core work and on a
// task whose text names no file under its repository's core directories
// (the Owner, 2026-11-06: a slip of form is fixed, not refused).
func TestPlanCoreLabel(t *testing.T) {
	t.Parallel()
	repos := []string{"bifrost-platform", "bifrost-trade-infra"}
	first := func(core, text, repo string) string {
		r := strings.Replace(goodPlan, `"text": "Say which ref MainHead reads.",`, `"text": "`+text+`", "core": `+core+`,`, 1)
		r = strings.Replace(r, `"title": "Document MainHead", "repo": "bifrost-platform"`, `"title": "Document MainHead", "repo": "`+repo+`"`, 1)
		if repo != "bifrost-platform" { // infra takes no checks of a plan's own
			r = strings.Replace(r, `["go", "-C", "api", "test", "./internal/agentrt/agentrtapi/"], ["go", "-C", "api", "test", "-run", "TestGoal", "./internal/agentrt/agentrtapi/"]`, "", 1)
		}
		return r
	}
	const agentd = "In api/internal/agentd/executors.go, keep whole characters in oneLine."
	for name, c := range map[string]struct {
		report string
		core   bool
	}{
		"left out":           {goodPlan, false},
		"false":              {first("false", "Say which ref MainHead reads.", "bifrost-platform"), false},
		"true, agentd":       {corePlan, true},
		"true, the store":    {first("true", "Change ./api/internal/agentrt/store/route.go.", "bifrost-platform"), true},
		"true, cmd":          {first("true", "Add a flag to `api/cmd/agentd/main.go`.", "bifrost-platform"), true},
		"true, a directory.": {first("true", "Everything stays in api/internal/agentrt/store/.", "bifrost-platform"), true},
		"true, in brackets":  {first("true", "Two files (api/internal/agentd/run.go, api/internal/agentd/x.go).", "bifrost-platform"), true},
		"false, core files":  {first("false", agentd, "bifrost-platform"), false},
	} {
		p, err := store.ParsePlan([]byte(c.report), repos)
		if err != nil {
			t.Errorf("%s refused: %v", name, err)
			continue
		}
		if p.Tasks[0].IsCore() != c.core || p.Tasks[1].IsCore() {
			t.Errorf("%s: core %v, %v; want %v, false", name, p.Tasks[0].IsCore(), p.Tasks[1].IsCore(), c.core)
		}
	}
	for name, c := range map[string]struct{ report, why string }{
		"a string":       {first(`"true"`, agentd, "bifrost-platform"), `core "true" read as true`},
		"a number":       {first("1", agentd, "bifrost-platform"), "core 1 read as true"},
		"null":           {first("null", agentd, "bifrost-platform"), "core null read as true"},
		"an object":      {first(`{"core": true}`, agentd, "bifrost-platform"), "read as true"},
		"infra":          {first("true", "Fix scripts/agentrt/t26_probe.py.", "bifrost-trade-infra"), "core read as false: bifrost-trade-infra has no core work"},
		"the Console":    {first("true", "Change console/src/pages/Crew.tsx.", "bifrost-platform"), "core read as false: its text names no file under"},
		"no path":        {first("true", "Make agentd keep whole characters.", "bifrost-platform"), "core read as false: its text names no file"},
		"a longer name":  {first("true", "Change api/internal/agentdx/a.go.", "bifrost-platform"), "core read as false: its text names no file"},
		"inside another": {first("true", "Change vendor/api/internal/agentd/a.go.", "bifrost-platform"), "core read as false: its text names no file"},
		"a file named":   {first("true", "Change api/internal/agentd.go.", "bifrost-platform"), "core read as false: its text names no file"},
		// Review 426 B1, C1: a whole path from the repository's root, beneath
		// a core directory, not the directory alone.
		"the directory alone": {first("true", "Everything stays in api/internal/agentd.", "bifrost-platform"), "core read as false: its text names no file"},
		"only mentioned":      {first("true", "The term api/internal/agentrt is mentioned; change console/src/a.ts.", "bifrost-platform"), "core read as false: its text names no file"},
		"out by ..":           {first("true", "Change api/internal/agentd/../../other.go.", "bifrost-platform"), "core read as false: its text names no file"},
		"backslashes":         {first("true", `Change api\\internal\\agentd\\a.go.`, "bifrost-platform"), "core read as false: its text names no file"},
		"behind a backslash":  {first("true", `Change vendor\\api/internal/agentd/a.go.`, "bifrost-platform"), "core read as false: its text names no file"},
		"a URL's query":       {first("true", "See https://example.com/?path=api/internal/agentd/a.go.", "bifrost-platform"), "core read as false: its text names no file"},
		"a URL's path":        {first("true", "See https://example.com/api/internal/agentd/a.go.", "bifrost-platform"), "core read as false: its text names no file"},
		"from the root":       {first("true", "Change /api/internal/agentd/a.go.", "bifrost-platform"), "core read as false: its text names no file"},
	} {
		p, err := store.ParsePlan([]byte(c.report), repos)
		if err != nil || p.Tasks[0].IsCore() != strings.HasSuffix(c.why, "read as true") || !hasNote(p, "tasks[0]: core", c.why) {
			t.Errorf("%s: %v core %v, notes %q (want %q)", name, err, err == nil && p.Tasks[0].IsCore(), p.Notes, c.why)
		}
	}
	// Review 426 C2: a key named twice in one object, in any case, is refused
	// (the decoder would keep the last); C3: a refused value is quoted cut.
	for name, report := range map[string]string{
		"core twice":        strings.Replace(corePlan, `"core": true,`, `"core": true, "core": false,`, 1),
		"Core and core":     strings.Replace(corePlan, `"core": true,`, `"core": true, "Core": false,`, 1),
		"a bad then a good": strings.Replace(corePlan, `"core": true,`, `"core": null, "core": true,`, 1),
		"a title twice":     strings.Replace(goodPlan, `"title": "Document MainHead",`, `"title": "Document MainHead", "title": "x",`, 1),
	} {
		if _, err := store.ParsePlan([]byte(report), repos); err == nil || !strings.Contains(err.Error(), "twice in one object") {
			t.Errorf("%s: %v", name, err)
		}
	}
	long := first(strings.Repeat("9", 100000), "Change api/internal/agentd/a.go.", "bifrost-platform")
	if p, err := store.ParsePlan([]byte(long), repos); err != nil || len(p.Notes) != 1 || len(p.Notes[0]) > 200 {
		t.Errorf("a long value read: %v %d notes", err, len(p.Notes))
	}
	// The same keys in different objects, and arrays of objects, still read.
	if _, err := store.ParsePlan([]byte(goodPlan), repos); err != nil {
		t.Errorf("goodPlan: %v", err)
	}
}
