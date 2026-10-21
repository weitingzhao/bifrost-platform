package agentd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TD-405: what the prompt says of each kind of failed check. The section is
// only inserted after the spec: without failed checks the prompt is what it
// was, which is all an exam's or a bench's attempt ever has.
func TestPriorChecksInThePrompt(t *testing.T) {
	verify := func(cmds ...CommandTrace) []byte {
		b, err := json.Marshal(Verification{Commands: cmds})
		must(t, err)
		return b
	}
	review := "## Findings\n\nA1: the test imports go/token beside `token` (```go``` block below)\n```go\nimport \"go/token\"\n```\n" +
		strings.Repeat("more\n", 5000) + "VERDICT: FAIL"
	plain := &Attempt{Claim: &Claimed{Role: "implement"}, Spec: TaskSpec{Text: "# Do it"}}
	for name, c := range map[string]struct {
		check    PriorCheck
		want     []string
		not      string
		maxBytes int
	}{
		"vet": {PriorCheck{FailedCheck{"deterministic", 2, "a"}, verify(CommandTrace{Argv: []string{"go", "-C", "api", "build", "./..."}},
			CommandTrace{Argv: []string{"go", "-C", "api", "vet", "./..."}, ExitCode: 1, Tail: "# x\nx_test.go:9:2: token redeclared in this block\n"})},
			[]string{"The deterministic check of attempt 2 failed. First failed command: go -C api vet ./... exited 1. Its output ends:\n\n```\n# x\nx_test.go:9:2: token redeclared in this block\n```\n"},
			"go -C api build", 0},
		"timed out, silent": {PriorCheck{FailedCheck{"deterministic", 1, "a"}, verify(CommandTrace{Argv: []string{"npm", "test"}, ExitCode: -1, TimedOut: true})},
			[]string{"First failed command: npm test exited -1, timed out; it wrote nothing.\n"}, "Its output", 0},
		"no command failed": {PriorCheck{FailedCheck{"deterministic", 1, "a"}, []byte(`{"pass":false,"commands":[],"reason":"the spec names no verify command"}`)},
			[]string{"No command failed; its reason: the spec names no verify command\n"}, "", 0},
		"not verify.json": {PriorCheck{FailedCheck{"deterministic", 1, "a"}, []byte("plain words")},
			[]string{"failed. Its report:\n\n```\nplain words\n```\n"}, "", 0},
		"review": {PriorCheck{FailedCheck{"review", 3, "a"}, []byte(review)},
			[]string{"The review of attempt 3 failed it. Its report:\n\n````\n## Findings\n\nA1: the test imports go/token", "(cut: the report is "},
			"VERDICT: FAIL", priorReviewBytes + 512},
	} {
		with := *plain
		with.Failed = []PriorCheck{c.check}
		p := Prompt(&with)
		head := "# Do it\n\n---\nThe last round failed its check; fix that first."
		for _, want := range append(c.want, head) {
			if !strings.Contains(p, want) {
				t.Errorf("%s: the prompt lacks %q:\n%s", name, want, p)
			}
		}
		if c.not != "" && strings.Contains(p, c.not) {
			t.Errorf("%s: the prompt has %q:\n%s", name, c.not, p)
		}
		section := p[len("# Do it"):strings.Index(p, "\n\n---\nWork only in this directory")]
		if c.maxBytes > 0 && len(section) > c.maxBytes {
			t.Errorf("%s: %d bytes of what failed, more than %d", name, len(section), c.maxBytes)
		}
		if strings.Replace(p, section, "", 1) != Prompt(plain) {
			t.Errorf("%s: the prompt changed beyond the inserted section", name)
		}
	}
	if p := Prompt(plain); strings.Contains(p, "last round") {
		t.Errorf("a first round is told of a last one:\n%s", p)
	}
}

// Only an implementation reads what earlier rounds failed: a check's attempt
// and a plan's, whatever their claim names, fetch nothing (this daemon has
// no client to fetch with).
func TestPriorChecksOnlyForAnImplementation(t *testing.T) {
	d := &Daemon{}
	named := []FailedCheck{{Method: "deterministic", AttemptNo: 1, Artifact: "sha256:x"}}
	for name, a := range map[string]*Attempt{
		"a review": {Claim: &Claimed{Role: "review", FailedChecks: named}, Spec: TaskSpec{Check: "review"}},
		"a check":  {Claim: &Claimed{Role: "verify", FailedChecks: named}, Spec: TaskSpec{Check: "verify"}},
		"a plan":   {Claim: &Claimed{Role: "plan", FailedChecks: named}},
		"no names": {Claim: &Claimed{Role: "implement"}},
	} {
		if got, err := d.priorChecks(context.Background(), a); err != nil || got != nil {
			t.Errorf("%s: %+v %v", name, got, err)
		}
	}
}

// Review td404-1 A1: the prompt is one argument of the executor's command,
// which Linux holds to 128 KiB with its NUL. What failed is fitted into what
// the spec and the rest leave, cut to a smaller share, or left out when not
// even the shortest fits: a retry starts wherever its first round could. An
// output's tail is never quoted longer than agentd keeps one.
func TestPriorChecksFitTheArgument(t *testing.T) {
	ticks := strings.Repeat("`", 40)
	review := []byte(ticks + "\n" + strings.Repeat("finding\n", 12<<10/8) + "VERDICT: FAIL")
	line := "x_test.go:9:2: token redeclared in this block\n"
	verify, err := json.Marshal(Verification{Commands: []CommandTrace{{Argv: []string{"go", "-C", "api", "vet", "./..."}, ExitCode: 1,
		Tail: strings.Repeat(line, 200)}}})
	must(t, err)
	checks := []PriorCheck{{FailedCheck{"deterministic", 2, "a"}, verify}, {FailedCheck{"review", 1, "b"}, review}}
	rest := promptRest(&Attempt{Claim: &Claimed{Role: "implement"}})
	for name, c := range map[string]struct {
		text      string
		told, cut bool
	}{
		"a short spec":               {"# Do it", true, false},
		"a large spec":               {strings.Repeat("spec line\n", 112<<10/10), true, true},
		"a spec that leaves no room": {strings.Repeat("s", promptMax-len(rest)-300), false, false},
	} {
		plain := &Attempt{Claim: &Claimed{Role: "implement"}, Spec: TaskSpec{Text: c.text}}
		with := *plain
		with.Failed = checks
		p := Prompt(&with)
		if len(p) > promptMax {
			t.Errorf("%s: a prompt of %d bytes, over the %d an argument holds", name, len(p), promptMax)
		}
		section := p[len(c.text) : len(p)-len(rest)]
		if p[:len(c.text)]+p[len(p)-len(rest):] != Prompt(plain) {
			t.Errorf("%s: the prompt changed beyond the inserted section", name)
		}
		if told := strings.Contains(section, "The last round failed its check"); told != c.told {
			t.Errorf("%s: told %v, want %v; %d bytes of section", name, told, c.told, len(section))
		}
		if !c.told {
			continue
		}
		if n := strings.Count(section, line); n == 0 || n*len(line) > tailBytes {
			t.Errorf("%s: %d lines of the output quoted, more than agentd keeps (%d bytes)", name, n, tailBytes)
		}
		for _, want := range []string{"First failed command: go -C api vet ./... exited 1", "(cut: the output is", ticks + "`\n" + ticks + "\n",
			"The review of attempt 1 failed it."} {
			if !strings.Contains(section, want) {
				t.Errorf("%s: the section lacks %q", name, want)
			}
		}
		if full := strings.Contains(section, "VERDICT: FAIL"); full == c.cut {
			t.Errorf("%s: the review's end quoted %v, want %v", name, full, !c.cut)
		}
	}
}
