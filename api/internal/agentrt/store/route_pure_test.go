package store

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Routing decisions as pure functions, without a database (review 155 B2).

func ptr[T any](v T) *T { return &v }

var (
	claudeTool = Tool{Executor: "exec:claude", Provider: "anthropic", Model: "opus"}
	codexTool  = Tool{Executor: "exec:codex", Provider: "openai", Model: "gpt-x"}
)

func TestExclusionReasonsInOrder(t *testing.T) {
	in := routeInput{job: "reviewer", core: "review_core", subject: "t_S", needs: []string{"gpu"},
		facts: []restriction{
			{id: "f1", executor: "exec:claude", kind: "cannot", subject: "gpu"},
			{id: "f2", executor: "exec:codex", kind: "only", subject: "gpu", provider: ptr("openai"), grants: true},
			{id: "f3", executor: "exec:codex", kind: "only", subject: "review_core", grants: true},
		},
		attempts: []priorAttempt{{id: "a1", executor: "exec:cursor", provider: ptr("anthropic")}}}
	got := strings.Join(in.exclusion(claudeTool), "; ")
	want := "cannot or must_not on a need; a need only others may take; core work needs only review_core; not independent of the subject's attempts"
	if got != want {
		t.Fatalf("reasons = %s", got)
	}
	if r := in.exclusion(codexTool); len(r) != 0 {
		t.Fatalf("codex = %v", r)
	}
	// Review 186 A2: an only fact whose rule is no longer in force grants
	// nobody, and still keeps everyone else out.
	lapsed := in
	lapsed.facts = []restriction{in.facts[0], {id: "f2", executor: "exec:codex", kind: "only", subject: "gpu", provider: ptr("openai")},
		{id: "f3", executor: "exec:codex", kind: "only", subject: "review_core"}}
	if got := strings.Join(lapsed.exclusion(codexTool), "; "); got != "a need only others may take; core work needs only review_core" {
		t.Fatalf("codex under lapsed only facts: %s", got)
	}
	// The only fact on gpu binds openai only: codex through another provider is not a holder.
	if r := in.exclusion(Tool{Executor: "exec:codex", Provider: "other"}); len(r) != 1 || r[0] != "a need only others may take" {
		t.Fatalf("codex/other = %v", r)
	}
}

func TestNilProviders(t *testing.T) {
	// A fact with no provider binds every provider of its executor.
	in := routeInput{needs: []string{"gpu"}, facts: []restriction{{id: "f", executor: "exec:claude", kind: "must_not", subject: "gpu"}}}
	if r := in.exclusion(Tool{Executor: "exec:claude", Provider: "other"}); len(r) != 1 {
		t.Fatalf("a fact with no provider let another provider through: %v", r)
	}
	// An attempt with no recorded provider makes independence unprovable for everyone.
	in = routeInput{subject: "t_S", attempts: []priorAttempt{{id: "a", executor: "exec:cursor"}}}
	if r := in.exclusion(codexTool); len(r) != 1 || r[0] != "not independent of the subject's attempts" {
		t.Fatalf("unknown provider = %v", r)
	}
}

func TestInputKeepsOnlyFactsOnTheTask(t *testing.T) {
	cp := capabilities{candidates: map[string][]jobCandidate{"implementer": {{id: "c", status: "active", executor: "exec:claude",
		provider: "anthropic", pattern: "*"}}},
		facts: []restriction{
			{id: "elsewhere", executor: "exec:claude", kind: "cannot", subject: "gpu"},
			{id: "core", executor: "exec:claude", kind: "only", subject: "author_core", grants: true},
		}}
	in := cp.input(candidate{role: "implement", needs: []string{"ui"}}, nil)
	if len(in.facts) != 0 {
		t.Fatalf("facts on neither a need nor the core label: %+v", in.facts)
	}
	if route, _ := in.decide(); len(route) != 1 {
		t.Fatalf("route = %v", route)
	}
	in = cp.input(candidate{role: "implement", core: true}, nil)
	if len(in.facts) != 1 || in.facts[0].id != "core" || in.core != "author_core" {
		t.Fatalf("core input = %+v", in)
	}
	if in = cp.input(candidate{role: "verify", core: true}, nil); in.core != "review_core" || in.job != "verifier" {
		t.Fatalf("a verification of core work = %+v", in)
	}
}

func TestPatternMustMatch(t *testing.T) {
	jc := jobCandidate{executor: "exec:codex", provider: "openai", pattern: "gpt-*"}
	if !jc.fits(codexTool) || jc.fits(Tool{Executor: "exec:codex", Provider: "openai", Model: "o3"}) {
		t.Fatal("gpt-* must take gpt-x and not o3")
	}
	if (jobCandidate{executor: "exec:codex", provider: "openai", pattern: "["}).fits(codexTool) {
		t.Fatal("a malformed pattern matched")
	}
	if (jobCandidate{executor: "exec:codex", provider: "other", pattern: "*"}).fits(codexTool) {
		t.Fatal("another provider matched")
	}
}

// The canonical record read back gives the same input, decision and
// fingerprint; another algorithm's record is refused.
func TestRecordReplays(t *testing.T) {
	in := routeInput{job: "reviewer", subject: "t_S", needs: []string{"gpu"},
		candidates: []jobCandidate{
			{id: "a", status: "active", executor: "exec:claude", provider: "anthropic", pattern: "*", version: 3,
				rule: ruleRef{id: ptr("job:initial"), version: ptr(2)}},
			{id: "b", status: "probation", executor: "exec:codex", provider: "openai", pattern: "gpt-*"},
		},
		facts:    []restriction{{id: "f", executor: "exec:claude", kind: "cannot", subject: "gpu", rule: ruleRef{id: ptr("caps")}}},
		attempts: []priorAttempt{{id: "x", executor: "exec:cursor", provider: ptr("cursor-mix")}, {id: "y", executor: "exec:cursor"}}}
	doc, err := canon.Encode(in.record())
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	back, err := replayInput(v.(map[string]any))
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := in.fingerprint()
	fp2, _ := back.fingerprint()
	r1, e1 := in.decide()
	r2, e2 := back.decide()
	d1, _ := canon.Encode([]any{r1, e1})
	d2, _ := canon.Encode([]any{r2, e2})
	if fp != fp2 || string(d1) != string(d2) {
		t.Fatalf("replayed %s (%s), decided %s (%s)", d2, fp2, d1, fp)
	}
	rec := in.record()
	first := rec["candidates"].([]any)[0].(map[string]any)
	if r, _ := canon.Encode(first["rule"]); string(r) != `{"id":"job:initial","version":2}` || rec["facts"].([]any)[0].(map[string]any)["rule"] == nil {
		t.Fatalf("recorded rules = %s, %v", r, rec["facts"])
	}
	rec["algorithm"] = routeAlgorithm + 1
	if _, err := replayInput(rec); err == nil {
		t.Fatal("replayed a record of another algorithm")
	}
}

// W-31 2.4 slice ② ⑤: below the threshold over enough samples is out; with
// too few, a candidate on probation takes no core work; no threshold, no step.
func TestUnfit(t *testing.T) {
	cm := &competence{threshold: threshold{minPercent: 40, minSamples: 5}, samples: map[string]sample{
		"low": {attempts: 10, firstPass: 2}, "high": {attempts: 10, firstPass: 9}, "few": {attempts: 2, firstPass: 2}}}
	core, plain := routeInput{core: "author_core", competence: cm}, routeInput{competence: cm}
	for i, c := range []struct {
		jc   jobCandidate
		in   routeInput
		want string
	}{
		{jobCandidate{id: "low", status: "active"}, plain, "below the competence threshold"},
		{jobCandidate{id: "low", status: "probation"}, core, "below the competence threshold"},
		{jobCandidate{id: "high", status: "probation"}, core, ""},
		{jobCandidate{id: "few", status: "probation"}, core, "too few samples for core work"},
		{jobCandidate{id: "few", status: "probation"}, plain, ""},
		{jobCandidate{id: "few", status: "active"}, core, ""},
		{jobCandidate{id: "none", status: "probation"}, core, "too few samples for core work"},
		{jobCandidate{id: "low", status: "probation"}, routeInput{core: "author_core"}, ""},
	} {
		if got := strings.Join(c.in.unfit(c.jc), "; "); got != c.want {
			t.Errorf("case %d (%s, %s): %q, want %q", i, c.jc.id, c.jc.status, got, c.want)
		}
	}
}
