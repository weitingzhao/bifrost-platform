package rules_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// An envelope's external hosts (DESIGN-coordinator-landing-v3 §2.1): an
// optional list, closed as the rest of a rule, in the body only when given.

const externalHosts = `external_hosts:
  - host: coordinator-mbp
    pin: external:coordinator
    proposer: seat:coordinator
    executor: exec:claude
    provider: anthropic
    repos: [bifrost-platform, bifrost-ui]
    roles: [implement]
`

var withHosts = envelope + externalHosts

func TestEnvelopeExternalHosts(t *testing.T) {
	t.Parallel()
	r := parse(t, withHosts)
	h, ok := r.Envelope.Pinned(rules.PinCoordinator)
	if !ok || h.Host != "coordinator-mbp" || h.Proposer != "seat:coordinator" || h.Executor != "exec:claude" || h.Provider != "anthropic" ||
		strings.Join(h.Repos, ",") != "bifrost-platform,bifrost-ui" || strings.Join(h.Roles, ",") != "implement" {
		t.Fatalf("entry %+v %v", h, ok)
	}
	if _, ok := r.Envelope.Pinned("external:other"); ok {
		t.Fatal("another pin has an entry")
	}
	body, err := r.BodyJSON()
	if err != nil || !strings.Contains(string(body), `"external_hosts":[{"executor":"exec:claude","host":"coordinator-mbp","pin":"external:coordinator",`+
		`"proposer":"seat:coordinator","provider":"anthropic","repos":["bifrost-platform","bifrost-ui"],"roles":["implement"]}]`) {
		t.Fatalf("body %s %v", body, err)
	}
	// Without it the body is what it always was.
	plain, err := parse(t, envelope).BodyJSON()
	if err != nil || strings.Contains(string(plain), "external_hosts") {
		t.Fatalf("an envelope without external hosts: %s %v", plain, err)
	}
	if _, ok := parse(t, envelope).Envelope.Pinned(rules.PinCoordinator); ok {
		t.Fatal("an envelope without external hosts has an entry")
	}
	// Each refusal is one change from the entry that reads.
	for name, edit := range map[string][3]string{
		"another pin":         {"pin: external:coordinator", "pin: external:other", "the one pin there is"},
		"a proposer no seat":  {"proposer: seat:coordinator", "proposer: thread:s1", "not a seat"},
		"an executor no exec": {"executor: exec:claude", "executor: claude", "not an executor"},
		"no provider":         {"    provider: anthropic\n", "", `needs "provider"`},
		"no repos":            {"[bifrost-platform, bifrost-ui]", "[]", "at least one repository"},
		"a repo twice":        {"[bifrost-platform, bifrost-ui]", "[bifrost-platform, bifrost-platform]", "listed twice"},
		"a review role":       {"roles: [implement]", "roles: [implement, review]", `"review" is none of`},
		"no roles":            {"roles: [implement]", "roles: []", "roles lists at least one"},
		"an unknown field":    {"roles: [implement]\n", "roles: [implement]\n    lease: forever\n", `"lease" is not a field`},
		"an empty list":       {externalHosts, "external_hosts: []\n", "leave it out for none"},
		"the entry twice":     {externalHosts, externalHosts + strings.TrimPrefix(externalHosts, "external_hosts:\n"), "listed twice"},
	} {
		if !strings.Contains(withHosts, edit[0]) {
			t.Fatalf("%s: no %q", name, edit[0])
		}
		text := strings.Replace(withHosts, edit[0], edit[1], 1)
		if _, err := rules.Parse([]byte(text)); err == nil || !strings.Contains(err.Error(), edit[2]) {
			t.Errorf("%s: %v; want %q", name, err, edit[2])
		}
	}
}

// Dropping an entry, or the list, tightens; an entry that grants anything
// more, or names another proposer, executor or provider, does not.
func TestExternalHostsTighten(t *testing.T) {
	t.Parallel()
	next := func(from, to string) string {
		s := strings.Replace(withHosts, "rule_version: 1\ndirection: initial", "rule_version: 2\ndirection: tighten", 1)
		return strings.Replace(s, from, to, 1)
	}
	for name, c := range map[string]struct {
		prev, next string
		tighter    bool
	}{
		"the same entry":       {withHosts, next("", ""), true},
		"the list dropped":     {withHosts, next(externalHosts, ""), true},
		"a repository dropped": {withHosts, next("[bifrost-platform, bifrost-ui]", "[bifrost-platform]"), true},
		"a repository added":   {withHosts, next("[bifrost-platform, bifrost-ui]", "[bifrost-platform, bifrost-ui, bifrost-trade-infra]"), false},
		"another proposer":     {withHosts, next("seat:coordinator", "seat:chief_of_staff"), false},
		"another executor":     {withHosts, next("executor: exec:claude", "executor: exec:codex"), false},
		"another provider":     {withHosts, next("provider: anthropic", "provider: openai"), false},
		"another host":         {withHosts, next("host: coordinator-mbp", "host: other-mac"), false},
		"the list added": {envelope, strings.Replace(withHosts, "rule_version: 1\ndirection: initial",
			"rule_version: 2\ndirection: tighten", 1), false},
	} {
		err := rules.Tightens(parse(t, c.prev), parse(t, c.next))
		if (err == nil) != c.tighter {
			t.Errorf("%s: tightens %v (%v); want %v", name, err == nil, err, c.tighter)
		}
	}
}
