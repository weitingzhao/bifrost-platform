import { z } from 'zod'
import { PROPOSE_MISSION_INPUT, RUNTIME_DATA_NOTE, defaultRuntimeDeps, isRFC3339, type RuntimeDeps } from './runtimeTools.js'

// The crew's issue tools (W-31 BI-1 slice 2, DESIGN-crew-intake §2.2): what
// a local thread works business issues with, on the platform's
// /agentrt/issues routes. The platform opens them to the issue readers only
// (the operator and admin tokens on its readers list) and records each write
// as this thread (thread:<X-Bifrost-Session>), or as the Owner for his admin
// token. Like the runtime's other tools they are runtime state, not cluster
// actions: they do not go through the approvals write gate, and they refuse
// when the session id fell back or MCP_WRITES=off.

const BASE = '/api/v1/agentrt/issues'

/** What every issue tool says of the texts it carries and reads. */
export const ISSUE_DATA_NOTE =
  `${RUNTIME_DATA_NOTE} A person's words are hers: they stay on the issue routes. Never copy them into a mission, a task, an artifact, a message or a card; a mission's material is what the work needs, written by the crew.`

export const ISSUE_KEY_RULE =
  'idempotency_key names this one write: reuse it only to retry the same request; a new write needs a new key. expected_version is the issue version you read: a write on another answers 409 with the issue as it is now.'

const WRITES_OFF = 'MCP_WRITES=off sends no agent runtime writes. Leave MCP_WRITES unset, or set it to on. No request was sent.'
const NO_SESSION = 'No session id is set. Refusing to write, because the platform records the write as this thread. No request was sent.'

function refuse(d: RuntimeDeps): { error: string; hint: string } | undefined {
  if (d.fellBack()) return { error: 'no session id', hint: NO_SESSION }
  if (d.mode() === 'off') return { error: 'writes not cut over', hint: WRITES_OFF }
  return undefined
}

function note(body: unknown): unknown {
  if (body == null || typeof body !== 'object' || Array.isArray(body)) return { note: ISSUE_DATA_NOTE, body }
  return { ...(body as Record<string, unknown>), note: ISSUE_DATA_NOTE }
}

const seg = (s: string) => encodeURIComponent(s)

const issueId = () => z.string().regex(/^BI-[1-9][0-9]*$/, 'an issue id, BI-<n>')
const version = () => z.number().int().min(0)
const key = () => z.string().min(1).describe('One key per write; see the description')
const when = () => z.string().refine(isRFC3339, 'an RFC 3339 date and time').optional()

const WORDS = z
  .array(z.object({ text: z.string().min(1).max(2000), said_at: when(), where: z.string().min(1).max(200).optional() }))
  .max(20)
  .describe("Her own words, verbatim, each with when and where she said them; never your summary")

export const FILE_ISSUE_INPUT = {
  product: z.string().regex(/^[a-z][a-z0-9-]{0,31}$/).describe('The product key (screening, finance, …)'),
  kind: z.enum(['complaint', 'question', 'request', 'proposal']),
  on_behalf_of: z.string().describe("person:owner, person:<name> when relaying the Owner's relay, or self (a proposal or question of the crew's own)"),
  title: z.string().min(1).max(80),
  person_words: WORDS.optional(),
  agent_reading: z.string().min(1).max(2000).optional().describe('What you understand, apart from her words'),
  agent_proposal: z.string().min(1).max(2000).optional().describe('What you propose, apart from her words'),
  dedupe_key: z.string().min(1).max(200).optional(),
  channel: z.enum(['thread', 'owner_relay']).optional().describe("thread: the Owner said it here; owner_relay: he relays someone else's words"),
  idempotency_key: key(),
}

export const ADD_TO_ISSUE_INPUT = {
  issue_id: issueId(),
  person_words: WORDS.optional(),
  agent_reading: z.string().min(1).max(2000).optional(),
  agent_proposal: z.string().min(1).max(2000).optional(),
  channel: z.enum(['thread', 'owner_relay']).optional(),
  idempotency_key: key(),
}

export const RELAY_INPUT = {
  issue_id: issueId(),
  expected_version: version(),
  in_reply_to: z.number().int().min(1).describe('The ask_id of the ask she answers'),
  user_words: z.string().min(1).max(2000).describe('Her answer, verbatim'),
  said_at: when(),
  user_verdict: z.enum(['confirmed', 'rejected', 'partly', 'withdrawn', 'answered']),
  agent_note: z.string().min(1).max(2000).optional().describe('Your own remark, kept apart from her words'),
  channel: z.enum(['thread', 'owner_relay']).optional(),
  idempotency_key: key(),
}

export const LIST_ISSUES_INPUT = {
  view: z.enum(['new', 'triage', 'active', 'awaiting_user', 'parked', 'closed']).optional(),
  state: z.string().optional(),
  product: z.string().optional(),
  waiting_on: z.enum(['crew', 'user', 'owner']).optional(),
  before: issueId().optional().describe('List below this issue (paging)'),
  limit: z.number().int().min(1).max(500).optional(),
}

const onIssue = { issue_id: issueId(), expected_version: version(), idempotency_key: key() }

export const DRAFT_BRIEF_INPUT = { ...onIssue, text: z.string().min(1).describe('The brief: for whom, the problem, success, in and out of scope, constraints, open questions') }

export const CLASSIFY_INPUT = {
  ...onIssue,
  risk_class: z.enum(['small', 'medium', 'large']),
  category: z.enum(['guidance', 'improvement', 'method_change', 'data_not_shown', 'fault', 'out_of_scope']),
  seat_report: z.string().regex(/^sha256:[0-9a-f]{64}$/).describe("The seat's answer: the report of a brief task"),
  reason: z.string().min(1).optional(),
}

export const PUT_DESIGN_INPUT = {
  ...onIssue,
  text: z.string().min(1),
  decisions: z
    .array(z.object({ decision: z.string().regex(/^D[1-9][0-9]*$/), proceed: z.array(z.string().regex(/^[A-Z]$/)).min(1) }))
    .optional()
    .describe('Each point the Owner decides and the letters under which the work goes on; what each letter means is in the text'),
}

export const RECORD_REVIEW_INPUT = {
  ...onIssue,
  report: z.string().min(1).describe('The review report in full; its last line says 通过 or 先修'),
  reviewer_tool: z.string().min(1),
  author_tool: z.string().min(1),
  round: z.number().int().min(1),
}

export const CLOSE_CHECK_INPUT = {
  ...onIssue,
  ask_id: z.number().int().min(1),
  result: z.enum(['supports', 'does_not']),
  seat_report: z.string().regex(/^sha256:[0-9a-f]{64}$/),
  excerpt: z.string().min(1).optional(),
}

// request_owner takes no text: the card's words are the platform's template
// (§5.3, R2-A2). An extra field is dropped by the schema, never sent.
export const REQUEST_OWNER_INPUT = {
  ...onIssue,
  purpose: z.enum(['priority', 'confirm_brief', 'design_decision', 'review_dispute']),
  decision: z.string().regex(/^D[1-9][0-9]*$/).optional().describe('design_decision: the point'),
  letters: z.number().int().min(2).max(4).optional().describe('design_decision: how many letters the point offers'),
}

const { work_id: _w, idempotency_key: _k, ...missionFields } = PROPOSE_MISSION_INPUT
void _w
void _k

export const TAKE_ISSUE_INPUT = {
  ...onIssue,
  commitment: z.string().min(1).describe('What the crew commits to deliver'),
  mission: z.object(missionFields).describe("The mission to make for it (its work id is the issue's): engineering material only"),
}

export const REPLY_INPUT = {
  ...onIssue,
  purpose: z.enum(['reply', 'needs_info', 'check_answer', 'ready_to_try']),
  text: z.string().min(1).describe('What goes back to the side that brought it'),
  commitment: z.number().int().min(1).optional().describe('ready_to_try: the commitment delivered'),
  mission_id: z.string().optional().describe('ready_to_try: its mission'),
  env: z.enum(['dev', 'stg', 'prod']).optional(),
  commits: z.array(z.string().regex(/^[0-9a-f]{40}$/)).optional(),
}

export const CLOSE_INPUT = {
  ...onIssue,
  conclusion: z.enum(['resolved', 'partial', 'already_possible', 'declined']),
  ask_id: z.number().int().min(1).optional().describe('The ask the conclusion rests on'),
  reason: z.string().min(1).optional(),
}

export const NOTE_INPUT = { ...onIssue, reason: z.string().min(1).optional() }

export const MERGE_INPUT = {
  from: issueId(),
  into: issueId(),
  from_version: version(),
  into_version: version(),
  idempotency_key: key(),
}

async function write(path: string, body: Record<string, unknown>, d: RuntimeDeps): Promise<unknown> {
  const refused = refuse(d)
  if (refused) return refused
  return note(await d.send('POST', path, body))
}

/** A write on one issue: its id goes in the path. */
async function onIssueWrite(action: string, input: Record<string, unknown> & { issue_id: string }, d: RuntimeDeps): Promise<unknown> {
  const { issue_id, ...body } = input
  return write(`${BASE}/${seg(issue_id)}/${action}`, body, d)
}

export const fileIssue = (input: Record<string, unknown>, d: RuntimeDeps = defaultRuntimeDeps()) => write(BASE, input, d)
export const addToIssue = (input: Record<string, unknown> & { issue_id: string }, d: RuntimeDeps = defaultRuntimeDeps()) =>
  onIssueWrite('additions', input, d)
export const relayUserResponse = (input: Record<string, unknown> & { issue_id: string }, d: RuntimeDeps = defaultRuntimeDeps()) =>
  onIssueWrite('responses', input, d)
export const mergeIssues = (input: Record<string, unknown>, d: RuntimeDeps = defaultRuntimeDeps()) => write(`${BASE}/merge`, input, d)

/** The writes on one issue, by tool name, and their route's last part. */
export const ISSUE_WRITES: Record<string, string> = {
  draft_brief: 'brief',
  classify_issue: 'classification',
  put_design: 'design',
  record_review: 'reviews',
  record_close_check: 'close-checks',
  request_owner: 'owner-cards',
  take_issue: 'take',
  reply_to_requester: 'replies',
  close_issue: 'close',
  park_issue: 'park',
  resume_issue: 'resume',
  reopen_issue: 'reopen',
}

export function issueWrite(tool: string) {
  const action = ISSUE_WRITES[tool]
  if (!action) throw new Error(`no issue write ${tool}`)
  return (input: Record<string, unknown> & { issue_id: string }, d: RuntimeDeps = defaultRuntimeDeps()) => onIssueWrite(action, input, d)
}

export async function listIssues(input: Record<string, string | number | undefined>, d: RuntimeDeps = defaultRuntimeDeps()): Promise<unknown> {
  const q = Object.entries(input)
    .filter(([, v]) => v !== undefined && v !== '')
    .map(([k, v]) => `${k}=${seg(String(v))}`)
  return note(await d.get(`${BASE}${q.length ? `?${q.join('&')}` : ''}`))
}

export async function getIssueFull(issue: string, d: RuntimeDeps = defaultRuntimeDeps()): Promise<unknown> {
  return note(await d.get(`${BASE}/${seg(issue)}`))
}
