import type { AuditRecord } from '@/api/auditTypes'

/** How many decisions the section lists and sums, newest first. */
export const POLICY_DECISION_LIMIT = 10

/** The two audit actions the release policy writes when it judges a run request (api/internal/server/release_policy_wire.go). */
const CHECK = 'release_policy.check'
const AUTO_APPROVE = 'release_policy.auto_approve'

/**
 * auto_approved: a direct call of a run that needs approval went through on the policy alone.
 * covered / not_covered: the policy was evaluated for a run that starts either way (STG).
 * unknown: a status this page does not know; it is shown as written.
 */
export type PolicyDecisionOutcome = 'auto_approved' | 'covered' | 'not_covered' | 'unknown'

export type PolicyDecision = {
  id: string
  at: string
  pipeline: string
  outcome: PolicyDecisionOutcome
  /** The audit record's status, as written. */
  status: string
  policyId: string
  requester: string
  commits: Array<{ repo: string; sha: string }>
  /** The policy's reasons, as written; empty when it gave none. */
  reasons: string
}

export type PolicyDecisionsView = {
  decisions: PolicyDecision[]
  covered: number
  autoApproved: number
  notCovered: number
  /** Newest auto-approval among all records read, not only the listed ones. */
  lastAutoApproved: PolicyDecision | null
  /** How far back the audit records that were read go. */
  recordsRead: number
  oldestRecordAt: string | null
}

const REASONS_KEY = ' reasons='

function field(head: string, key: string): string {
  const match = new RegExp(`(?:^|\\s)${key}=(\\S*)`).exec(head)
  return match?.[1] ?? ''
}

function outcomeOf(record: AuditRecord): PolicyDecisionOutcome {
  const status = record.status.trim().toLowerCase()
  if (record.action === AUTO_APPROVE) return status === 'approved' ? 'auto_approved' : 'unknown'
  if (status === 'covered') return 'covered'
  if (status === 'not covered') return 'not_covered'
  return 'unknown'
}

/** One audit record of a policy decision. The detail is `key=value` words, then `reasons=` to the end. */
export function policyDecision(record: AuditRecord): PolicyDecision {
  const detail = record.detail ?? ''
  const cut = detail.indexOf(REASONS_KEY)
  const head = cut < 0 ? detail : detail.slice(0, cut)
  const reasons = cut < 0 ? '' : detail.slice(cut + REASONS_KEY.length).trim()
  // The target is "<action id> <pipeline>".
  const target = record.target.trim()
  const space = target.indexOf(' ')
  const commits = field(head, 'sha')
    .split(',')
    .map(pair => pair.split('='))
    .filter(pair => pair.length === 2 && pair[0] !== '' && pair[1] !== '')
    .map(([repo, sha]) => ({ repo, sha }))
  return {
    id: record.id,
    at: record.at,
    pipeline: space < 0 ? target : target.slice(space + 1).trim(),
    outcome: outcomeOf(record),
    status: record.status,
    policyId: field(head, 'policy_id'),
    requester: field(head, 'requester'),
    commits,
    reasons,
  }
}

function instant(iso: string): number {
  const t = Date.parse(iso)
  return Number.isFinite(t) ? t : 0
}

/** The newest decisions of the audit records, with what they sum to. */
export function policyDecisionsView(records: readonly AuditRecord[], limit = POLICY_DECISION_LIMIT): PolicyDecisionsView {
  const all = records
    .filter(record => record.action === CHECK || record.action === AUTO_APPROVE)
    .map(policyDecision)
    .sort((a, b) => instant(b.at) - instant(a.at))
  const decisions = all.slice(0, limit)
  const count = (outcome: PolicyDecisionOutcome) => decisions.filter(d => d.outcome === outcome).length
  const autoApproved = count('auto_approved')
  let oldest: string | null = null
  for (const record of records) {
    if (instant(record.at) > 0 && (oldest == null || instant(record.at) < instant(oldest))) oldest = record.at
  }
  return {
    decisions,
    covered: autoApproved + count('covered'),
    autoApproved,
    notCovered: count('not_covered'),
    lastAutoApproved: all.find(d => d.outcome === 'auto_approved') ?? null,
    recordsRead: records.length,
    oldestRecordAt: oldest,
  }
}

/** "bifrost-platform f7bc5bf, bifrost-ui c83f40b" */
export function formatDecisionCommits(decision: PolicyDecision, full = false): string {
  if (decision.commits.length === 0) return '—'
  return decision.commits.map(c => `${c.repo} ${full ? c.sha : c.sha.slice(0, 7)}`).join(full ? '\n' : ', ')
}

export const POLICY_DECISION_LABEL: Record<Exclude<PolicyDecisionOutcome, 'unknown'>, string> = {
  auto_approved: 'covered · run started without approval',
  covered: 'covered · check only',
  not_covered: 'not covered',
}

/** "2 of the last 10 decisions covered" */
export function policyDecisionsHeadline(view: PolicyDecisionsView): string {
  const n = view.decisions.length
  if (n === 0) return 'No release policy decision in the audit records'
  return `${view.covered} of the last ${n} decision${n === 1 ? '' : 's'} covered`
}
