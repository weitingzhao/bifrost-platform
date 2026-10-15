package store_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// ruleText is a rule's text with its header.
func ruleText(id string, version int, direction, effective, expires, body string) string {
	return fmt.Sprintf("rule_id: %s\nrule_version: %d\ndirection: %s\neffective_at: %s\nexpires_at: %s\n%s",
		id, version, direction, effective, expires, body)
}

const (
	since = "2026-10-01T00:00:00Z"
	until = "2027-01-15T00:00:00Z"
)

func envelopeText(version int, direction, expires string, attempts int, executors string) string {
	return ruleText("standing-envelope:dev", version, direction, since, expires, fmt.Sprintf(`tenant: dev
max_action: branch
budget: {max_attempts: %d, max_wall_hours: 48}
executors: [%s]
hosts: [h1]
`, attempts, executors))
}

// signer signs rule texts with a key the test compiles in as the anchor.
type signer struct {
	t   *testing.T
	key rptest.Key
}

func (s signer) sign(w *world, text string) (map[string]any, error) {
	return store.SignRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, idOf(text), []byte(text),
		s.key.Sign(s.t, []byte(text), rules.Namespace), s.key.Fingerprint)
}

func idOf(text string) string {
	first, _, _ := strings.Cut(text, "\n")
	return strings.TrimPrefix(first, "rule_id: ")
}

func inForce(t *testing.T, w *world, id string, at time.Time) *store.StoredRule {
	t.Helper()
	var r *store.StoredRule
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		r, err = store.CurrentRule(ctx, tx, id, at)
		return err
	}))
	return r
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// T35: the rule set. v4's three (a loosening without a signature is refused,
// a tightening takes effect at once, an expired rule gives way to the
// strictest default) and v5's (body_json other than the text says, marked
// tighten but loosening, signed in the release policy's namespace, a field
// the parser does not know, no trust anchor).
func TestRuleSet(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	now := at("2026-11-01T00:00:00Z")
	if r := inForce(t, w, "standing-envelope:dev", now); r != nil {
		t.Fatalf("a rule in force before any was signed: %+v", r)
	}
	v1 := envelopeText(1, rules.Initial, until, 40, "exec:codex, exec:deterministic")
	out, err := s.sign(w, v1)
	ok(t, err)
	if out["rule_version"] != 1 || out["signed"] != true {
		t.Fatalf("signed %v", out)
	}
	if r := inForce(t, w, "standing-envelope:dev", now); r == nil || r.Envelope.Budget.MaxAttempts != 40 || r.SignedBy != s.key.Fingerprint {
		t.Fatalf("in force after signing: %+v", r)
	}
	tighten := func(text string) error {
		_, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), idOf(text), []byte(text))
		return err
	}
	// A loosening without a signature, whatever it calls itself.
	for name, text := range map[string]string{
		"marked tighten, more attempts":  envelopeText(2, rules.Tighten, until, 41, "exec:codex, exec:deterministic"),
		"marked tighten, a new executor": envelopeText(2, rules.Tighten, until, 40, "exec:codex, exec:claude"),
		"marked tighten, expiring later": envelopeText(2, rules.Tighten, "2027-03-01T00:00:00Z", 40, "exec:codex"),
		"marked loosen":                  envelopeText(2, rules.Loosen, until, 20, "exec:codex"),
	} {
		if err := tighten(text); !errors.Is(err, store.ErrUnsigned) {
			t.Errorf("%s: %v; want refused as unsigned", name, err)
		}
	}
	// Signed, the label still has to be true.
	if _, err := s.sign(w, envelopeText(2, rules.Tighten, until, 41, "exec:codex")); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("signed, marked tighten, loosening: %v; want refused", err)
	}
	// A tightening takes effect at once, unsigned; it expires sooner, and
	// once it has, the looser version before it does not come back.
	ok(t, tighten(envelopeText(2, rules.Tighten, "2026-12-15T00:00:00Z", 20, "exec:codex")))
	if r := inForce(t, w, "standing-envelope:dev", now); r == nil || r.Version != 2 || r.Envelope.Budget.MaxAttempts != 20 || r.SignedBy != "" {
		t.Fatalf("in force after the tightening: %+v", r)
	}
	if r := inForce(t, w, "standing-envelope:dev", at("2026-12-20T00:00:00Z")); r != nil {
		t.Fatalf("after the tightening expired, in force: v%d; want none (the strictest default)", r.Version)
	}
	// Versions follow one another.
	for name, text := range map[string]string{
		"a version skipped": envelopeText(4, rules.Tighten, "2026-12-15T00:00:00Z", 10, "exec:codex"),
		"a version again":   envelopeText(2, rules.Tighten, "2026-12-15T00:00:00Z", 10, "exec:codex"),
		"initial again":     envelopeText(1, rules.Initial, until, 10, "exec:codex"),
	} {
		if err := tighten(text); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
	// Signatures that are not the Owner's for a rule.
	v3 := envelopeText(3, rules.Loosen, until, 40, "exec:codex")
	other := rptest.NewKey(t)
	for name, c := range map[string]struct{ sig, anchor string }{
		"the release policy's namespace": {s.key.Sign(t, []byte(v3), releasepolicy.NamespacePolicy), s.key.Fingerprint},
		"another key":                    {other.Sign(t, []byte(v3), rules.Namespace), s.key.Fingerprint},
		"no trust anchor":                {s.key.Sign(t, []byte(v3), rules.Namespace), ""},
		"over other text":                {s.key.Sign(t, []byte(v3+"\n"), rules.Namespace), s.key.Fingerprint},
		"no signature":                   {"", s.key.Fingerprint},
	} {
		if _, err := store.SignRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "standing-envelope:dev", []byte(v3), c.sig, c.anchor); !errors.Is(err, store.ErrUnsigned) {
			t.Errorf("%s: %v; want refused as unsigned", name, err)
		}
	}
	// A field the parser does not know, signed or not.
	odd := envelopeText(3, rules.Loosen, until, 40, "exec:codex") + "approve_releases: true\n"
	if _, err := s.sign(w, odd); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("an unknown field: %v; want refused", err)
	}
	// An executor or host the runtime never registered.
	if _, err := s.sign(w, envelopeText(3, rules.Loosen, until, 40, "exec:codex, exec:gemini")); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("an unregistered executor: %v; want refused", err)
	}
	ok(t, firstErr(s.sign(w, v3)))
	if r := inForce(t, w, "standing-envelope:dev", now); r == nil || r.Version != 3 || r.Direction != rules.Loosen {
		t.Fatalf("in force after the signed loosening: %+v", r)
	}
	// A row whose body_json is not what its text says is no rule.
	bad := envelopeText(4, rules.Tighten, until, 30, "exec:codex")
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, effective_at, expires_at)
	  VALUES ('standing-envelope:dev', 4, '{"budget":{"max_attempts":1000}}', %s, 'tighten', '%s', '%s')`, quote(bad), since, until))
	if err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.CurrentRule(ctx, tx, "standing-envelope:dev", now)
		return err
	}); err == nil {
		t.Fatal("a rule whose body_json disagrees with its text was read")
	}
}

func firstErr(_ map[string]any, err error) error { return err }

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func jobText(version int, direction, candidates string) string {
	return ruleText("job:implementer", version, direction, since, until, `tenant: dev
text: Implement the task's spec on a branch.
candidates:
`+candidates)
}

func candidate(executor, provider, status string) string {
	return fmt.Sprintf("  - {executor: %s, provider: %s, model_pattern: \"*\", status: %s}\n", executor, provider, status)
}

func candidates(t *testing.T, w *world) string {
	t.Helper()
	var out string
	ok(t, w.rt.QueryRow(ctx, `SELECT string_agg(executor_actor_id || '/' || model_provider || '=' || status || '@' || rule_version,
	    ' ' ORDER BY executor_actor_id, model_provider) FROM agentrt.job_candidate WHERE job_id = 'implementer'`).Scan(&out))
	return out
}

// A job rule writes the job and its candidates: signed, it states them whole
// (one left out is retired); a tightening only lowers a status, and never
// above what the table has.
func TestJobRule(t *testing.T) {
	w := newWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, jobText(1, rules.Initial, candidate("exec:codex", "openai", "active")+candidate("exec:cursor", "cursor-mix", "probation")))))
	if got := candidates(t, w); got != "exec:codex/openai=active@1 exec:cursor/cursor-mix=probation@1" {
		t.Fatalf("after signing: %s", got)
	}
	ok(t, firstErr(s.sign(w, jobText(2, rules.Loosen, candidate("exec:codex", "openai", "active")+candidate("exec:claude", "anthropic", "qualifying")))))
	if got := candidates(t, w); got != "exec:claude/anthropic=qualifying@2 exec:codex/openai=active@2 exec:cursor/cursor-mix=retired@2" {
		t.Fatalf("after the second signing: %s", got)
	}
	// The table has codex demoted (an exam said so); a tightening to
	// probation leaves it demoted, and retires claude, left out.
	w.legacy(t, `UPDATE agentrt.job_candidate SET status = 'demoted' WHERE executor_actor_id = 'exec:codex'`)
	_, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), "job:implementer", []byte(jobText(3, rules.Tighten, candidate("exec:codex", "openai", "probation"))))
	ok(t, err)
	if got := candidates(t, w); got != "exec:claude/anthropic=retired@3 exec:codex/openai=demoted@2 exec:cursor/cursor-mix=retired@2" {
		t.Fatalf("after the tightening: %s", got)
	}
	var job string
	ok(t, w.rt.QueryRow(ctx, `SELECT tenant || '@' || rule_version FROM agentrt.job WHERE job_id = 'implementer'`).Scan(&job))
	if job != "dev@2" {
		t.Fatalf("the job is %s; want the brief of the last signed version", job)
	}
}

func factsText(version int, direction, facts string) string {
	return ruleText("capability-facts", version, direction, since, until, "facts:\n"+facts)
}

func liveFacts(t *testing.T, w *world) string {
	t.Helper()
	var out string
	ok(t, w.rt.QueryRow(ctx, `SELECT string_agg(executor_actor_id || ':' || fact_kind || ':' || subject || ':' || source_kind ||
	    COALESCE('@' || rule_version, ''), ' ' ORDER BY executor_actor_id, subject) FROM agentrt.capability_fact WHERE retired_at IS NULL`).Scan(&out))
	return out
}

// The capability-facts rule is the Owner's facts whole: the seed's that it
// lists are bound to it, the ones it leaves out are retired, measured facts
// are not its to touch; a tightening only adds cannot and must_not.
func TestCapabilityFactsRule(t *testing.T) {
	w := newWorld(t)
	s := signer{t, rptest.NewKey(t)}
	before := liveFacts(t, w)
	if !strings.Contains(before, "exec:cursor:must_not:fix_review_findings:owner") || !strings.Contains(before, "exec:claude:only:author_core:owner") {
		t.Fatalf("seed facts: %s", before)
	}
	ok(t, firstErr(s.sign(w, factsText(1, rules.Initial, `  - {executor: exec:cursor, kind: must_not, subject: fix_review_findings}
  - {executor: exec:claude, kind: only, subject: author_core, scope: {core: true}}
  - {executor: exec:claude, kind: cannot, subject: docker}
`))))
	got := liveFacts(t, w)
	for _, want := range []string{"exec:cursor:must_not:fix_review_findings:owner@1", "exec:claude:only:author_core:owner@1",
		"exec:claude:cannot:docker:owner@1", "exec:codex:cannot:git_commit:measured", "exec:codex:cannot:listen_port_tests:measured"} {
		if !strings.Contains(got, want) {
			t.Errorf("after signing, no %s in %s", want, got)
		}
	}
	if strings.Contains(got, "review_core") || strings.Contains(got, "review_own_provider") {
		t.Errorf("owner facts the rule leaves out are still live: %s", got)
	}
	// The seed's facts it lists are the same rows, bound to it.
	var bound int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.capability_fact WHERE capability_fact_id IN
	    ('core-author-claude', 'cursor-no-fix-review-findings') AND retired_at IS NULL AND rule_version = 1`).Scan(&bound))
	if bound != 2 {
		t.Errorf("%d seed facts bound to the rule; want 2", bound)
	}
	_, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), "capability-facts", []byte(factsText(2, rules.Tighten,
		`  - {executor: exec:cursor, kind: must_not, subject: fix_review_findings}
  - {executor: exec:claude, kind: only, subject: author_core, scope: {core: true}}
  - {executor: exec:claude, kind: cannot, subject: docker}
  - {executor: exec:codex, kind: must_not, subject: release}
`)))
	ok(t, err)
	if got = liveFacts(t, w); !strings.Contains(got, "exec:codex:must_not:release:owner@2") || !strings.Contains(got, "exec:claude:cannot:docker:owner@1") {
		t.Fatalf("after the tightening: %s", got)
	}
	_, err = store.TightenRule(ctx, w.rt, wr("thread:s1"), "capability-facts", []byte(factsText(3, rules.Tighten,
		`  - {executor: exec:cursor, kind: must_not, subject: fix_review_findings}
  - {executor: exec:codex, kind: must_not, subject: release}
`)))
	if !errors.Is(err, store.ErrUnsigned) {
		t.Fatalf("dropping a fact unsigned: %v; want refused", err)
	}
}

// Check and proposals write nothing that takes effect; a proposal keeps the
// text and says whether the Owner must sign it.
func TestRuleCheckAndProposal(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, envelopeText(1, rules.Initial, until, 40, "exec:codex"))))
	tighter := envelopeText(2, rules.Tighten, until, 20, "exec:codex")
	looser := envelopeText(2, rules.Loosen, until, 60, "exec:codex")
	for text, want := range map[string]bool{tighter: true, looser: false} {
		out, err := store.CheckRule(ctx, w.rt, "standing-envelope:dev", []byte(text))
		ok(t, err)
		if out["tightens"] != want || out["needs_signature"] == want || out["previous_version"] != 1 {
			t.Errorf("check: %v; want tightens %v", out, want)
		}
	}
	out, err := store.ProposeRule(ctx, w.rt, wr("thread:s1"), "r1", "standing-envelope:dev", []byte(looser), "the benchmarks need more attempts")
	ok(t, err)
	if out["needs_signature"] != true || out["text_artifact_id"] == "" {
		t.Fatalf("proposal: %v", out)
	}
	if r := inForce(t, w, "standing-envelope:dev", at("2026-11-01T00:00:00Z")); r == nil || r.Version != 1 {
		t.Fatalf("a proposal took effect: %+v", r)
	}
	if _, err := store.CheckRule(ctx, w.rt, "budget:dev", []byte(looser)); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("a text for another rule id: %v; want refused", err)
	}
}
