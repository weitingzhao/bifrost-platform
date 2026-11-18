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

// TuningRule is a tuning rule that sets one value (failures_to_alert: 2), as
// if the Owner had signed it: the version a capacity card's draft follows.
// It stands where ConcurrencyRule stood for those tests until the dispatch
// kinds were retired (2026-11-15).
const TuningRule = ruleInsert + `('tuning', 1,
    '{"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","rule_id":"tuning","rule_version":1,"values":{"failures_to_alert":2}}',
    E'rule_id: tuning\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\nvalues: {failures_to_alert: 2}\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z') ON CONFLICT DO NOTHING`

// CardAutoRule is a card-auto-answer rule (TD-390) that answers blocked
// cards 30 minutes after they open, as if the Owner had signed it on STG.
const CardAutoRule = ruleInsert + `('card-auto-answer', 1,
    '{"after_minutes":30,"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","kinds":["blocked"],"rule_id":"card-auto-answer","rule_version":1}',
    E'rule_id: card-auto-answer\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\nafter_minutes: 30\nkinds: [blocked]\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z') ON CONFLICT DO NOTHING`
