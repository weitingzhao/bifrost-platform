package agentrttest

// The rule rows tests insert as if the Owner had signed them. They name the
// rule table, a permission record, so this file is a permission path
// (rules.landingHardGo) while the rest of the harness stays open.

// The two rules the Owner signs that dispatch needs, as rows a test inserts
// as the runtime role: concurrency (one live attempt a host) and the dev
// tenant's budget (missions that state none get 1000 attempts and 720 hours;
// an attempt at most a day; auto_grant 0, which the runtime no longer
// reads). Without both in force the runtime dispatches
// nothing. Each row's text and body_json agree (store's TestFixtureRule reads
// them back).
const (
	ConcurrencyRule = ruleInsert + concurrencyRow + ` ON CONFLICT DO NOTHING`
	DevBudgetRule   = ruleInsert + devBudgetRow + ` ON CONFLICT DO NOTHING`
	DispatchRules   = ruleInsert + concurrencyRow + ",\n" + devBudgetRow + ` ON CONFLICT DO NOTHING`
)

const ruleInsert = `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
    effective_at, expires_at) VALUES `

const concurrencyRow = `('concurrency', 1,
    '{"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","per_host":1,"rule_id":"concurrency","rule_version":1}',
    E'rule_id: concurrency\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\nper_host: 1\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z')`

const devBudgetRow = `('budget:dev', 1,
    '{"attempt_hard_limit_s":86400,"auto_grant":{"max_attempts":0,"max_wall_hours":0},"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","mission":{"max_attempts":1000,"max_wall_hours":720},"rule_id":"budget:dev","rule_version":1,"tenant":"dev"}',
    E'rule_id: budget:dev\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\ntenant: dev\nmission: {max_attempts: 1000, max_wall_hours: 720}\nattempt_hard_limit_s: 86400\nauto_grant: {max_attempts: 0, max_wall_hours: 0}\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z')`

// TestJobRule is the job rule the test fixtures' candidates belong to
// (job:test): routing counts a candidate only while its rule is in force and
// reads.
const TestJobRule = ruleInsert + `('job:test', 1,
    '{"candidates":[{"executor":"exec:codex","model_pattern":"*","provider":"openai","status":"active"}],"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","rule_id":"job:test","rule_version":1,"tenant":"dev","text":"Test fixture candidates."}',
    E'rule_id: job:test\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\ntenant: dev\ntext: Test fixture candidates.\ncandidates:\n  - {executor: exec:codex, provider: openai, model_pattern: "*", status: active}\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z') ON CONFLICT DO NOTHING`

// OwnerFactsRule is the capability-facts rule with the seed's four Owner
// facts, as if he had signed it; BindOwnerFacts binds the seed rows to it (run
// it with an event of the transaction, the import event in tests). Until a
// capability-facts rule binds them, an only fact grants nobody.
const OwnerFactsRule = ruleInsert + `('capability-facts', 1,
    '{"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","facts":[{"executor":"exec:claude","kind":"only","scope":{"core":true},"subject":"author_core"},{"executor":"exec:codex","kind":"only","scope":{"core":true,"sandbox":"read-only"},"subject":"review_core"},{"executor":"exec:cursor","kind":"must_not","subject":"fix_review_findings"},{"executor":"exec:deterministic","kind":"must_not","subject":"review_own_provider"}],"rule_id":"capability-facts","rule_version":1}',
    E'rule_id: capability-facts\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\nfacts:\n  - {executor: exec:cursor, kind: must_not, subject: fix_review_findings}\n  - {executor: exec:claude, kind: only, subject: author_core, scope: {core: true}}\n  - {executor: exec:codex, kind: only, subject: review_core, scope: {core: true, sandbox: read-only}}\n  - {executor: exec:deterministic, kind: must_not, subject: review_own_provider}\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z') ON CONFLICT DO NOTHING`

// BindOwnerFacts binds the seed's Owner facts to OwnerFactsRule.
const BindOwnerFacts = `UPDATE agentrt.capability_fact SET rule_id = 'capability-facts', rule_version = 1
  WHERE source_kind = 'owner' AND rule_id IS NULL`

// CardAutoRule is a card-auto-answer rule (TD-390) that answers blocked
// cards 30 minutes after they open, as if the Owner had signed it on STG.
const CardAutoRule = ruleInsert + `('card-auto-answer', 1,
    '{"after_minutes":30,"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","kinds":["blocked"],"rule_id":"card-auto-answer","rule_version":1}',
    E'rule_id: card-auto-answer\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\nafter_minutes: 30\nkinds: [blocked]\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z') ON CONFLICT DO NOTHING`
