import '@testing-library/jest-dom/vitest'
import { describe, expect, it } from 'vitest'
import shared from '@/api/__fixtures__/plan-explain-shared-block.json'
import card from '@/api/__fixtures__/plan-explain-step-card.json'
import declined from '@/api/__fixtures__/plan-explain-declined.json'
import failed from '@/api/__fixtures__/plan-explain-failed.json'
import pending from '@/api/__fixtures__/plan-explain-pending.json'
import staleOk from '@/api/__fixtures__/plan-explain-stale-ok.json'
import { parsePlanExplain } from '@/api/planExplainSchema'
import { splitPlanExplain, type PlanSnapshotRead } from '@/api/planSnapshot'
import { approvalHref, goalHref, questionHref } from '@/lib/shell/consoleRoutes'
import { flatRows, stepWhy, type StepWhyModel, type WhyRow } from '../explainModel'
import { AT, r5Raw, T3 } from './moduleDetailFixture'

/*
 * R5:5.5-19 (DESIGN-r5-5.5-19-waiting-chain §6, §7) as laid out in R5:5.5-20: why a step in progress is not done, from
 * the plan view's explain block. The contract samples parse; each result and blocker says what its record is, links to
 * it, and reads whether anyone is pushing it from the fresh snapshot by the same id; nothing says whose turn it is.
 */
const STEP = 'R5:5.1-3'
const NOW = Date.parse(AT) + 5_000
const OP = `land:${T3}:2`
type Rec = Record<string, unknown>

/** The R5 fixture with an explain block, and the base snapshot changed by more. */
function viewWith(explain: unknown, more: (raw: ReturnType<typeof r5Raw>) => void = () => {}): PlanSnapshotRead {
  const raw = r5Raw()
  more(raw)
  return splitPlanExplain({ ...raw, explain }, 'R5')
}
const version = () => viewWith(undefined).nodes.find(n => n.plan_node_id === STEP)!.version
const ok = (step: Rec | null, readAt = AT) => ({ status: 'ok', read_at: readAt, steps: step ? { [STEP]: { node_version: version(), ...step } } : {} })
const explained = (blockers: unknown[], rule = 'false', readAt = AT) => ok({ kind: 'explained', rule, blockers }, readAt)
/** A read a minute before the plan view's snapshot: the snapshot is the later read. */
const EARLIER = new Date(Date.parse(AT) - 60_000).toISOString()
const at = (ms: number) => new Date(Date.parse(AT) + ms).toISOString()
const why = (v: PlanSnapshotRead, old = '') => stepWhy(v, v.nodes.find(n => n.plan_node_id === STEP)!, NOW, old)
const said = (v: PlanSnapshotRead) => { const w = why(v)!; return `${w.word} ${w.reason}` }
const rows = (v: PlanSnapshotRead): WhyRow[] => flatRows(why(v)?.list?.entries ?? [])
const first = (v: PlanSnapshotRead) => rows(v)[0]
const card1 = (more: Rec = {}) => ({ question_id: null, prep: 'root_failed:7:1', kind: 'root_failed', family: 'landing', ask_started: false, closed: false, closed_why: null, ...more })
const pendingLanding = (more: Rec = {}) => ({ atom: 'step_landed', state: 'landing_pending', task_id: T3, op: OP, phase: 'branch_pushed',
  last_record_at: new Date(NOW - 60_000).toISOString(), grant: null, cards: [], ...more })
const allWords = (w: StepWhyModel) => [w.word, w.reason,
  ...flatRows(w.list?.entries ?? []).flatMap(r => [r.what, r.sub, r.record, r.push.word, r.request?.text ?? ''])].join(' | ')

describe('the explain block (contract §7)', () => {
  it('reads the samples: a declined root with its card, pending, failed cold, an old success with a failure, the step card', () => {
    for (const sample of [declined, pending, failed, staleOk, card, shared]) expect(parsePlanExplain(sample).ok).toBe(true)
    const d = parsePlanExplain(declined)
    if (!d.ok) throw new Error(d.error)
    const s = d.explain.steps['R5:5.5-10']
    expect(s.kind === 'explained' && s.blockers[0]).toMatchObject({ state: 'landing_declined', rework: { seen: { current: true }, card: { question_id: 'q_0123456789abcdef', outcome: null } } })
    const f = parsePlanExplain(failed)
    expect(f.ok && f.explain.status === 'failed' && f.explain.reason === 'input_too_large' && !f.explain.read_at && Object.keys(f.explain.steps)).toEqual([])
    const st = parsePlanExplain(staleOk)
    expect(st.ok && st.explain.last_error).toBe('timeout')
  })

  it('is closed: a key outside the contract, or a state it does not name, refuses the block; a word it does not know does not', () => {
    const bad = explained([{ atom: 'step_landed', state: 'landing_failed', task_id: T3, op: OP, outcome: 'stale', cards: [], extra: 1 }])
    expect(parsePlanExplain(bad).ok).toBe(false)
    expect(parsePlanExplain(explained([{ atom: 'step_landed', state: 'landing_somewhere', task_id: T3 }])).ok).toBe(false)
    expect(parsePlanExplain({ status: 'ok', steps: {} }).ok).toBe(false) // ok needs its read time
    expect(parsePlanExplain(explained([pendingLanding({ phase: 'a_phase_added_later' })])).ok).toBe(true)
    // R5:5.5-20: since, optional on a task candidate and an ended landing, and a time when given; nothing else is added.
    const short = (c: Rec) => explained([{ atom: 'step_tasks', state: 'tasks_short', have: 0, need: 1, candidates: [{ task_id: T3, task_state: 'ready', ...c }] }])
    expect(parsePlanExplain(short({ since: AT })).ok).toBe(true)
    expect(parsePlanExplain(short({})).ok).toBe(true)
    expect(parsePlanExplain(short({ since: 'yesterday' })).ok).toBe(false)
    expect(parsePlanExplain(short({ updated_at: AT })).ok).toBe(false)
    expect(parsePlanExplain(explained([{ atom: 'step_landed', state: 'landing_failed', task_id: T3, op: OP, outcome: 'stale', cards: [], since: AT }])).ok).toBe(true)
    expect(parsePlanExplain(explained([{ atom: 'step_landed', state: 'landing_pending', task_id: T3, op: OP, phase: 'x', last_record_at: null, grant: null, cards: [], since: AT }])).ok).toBe(false)
  })

  it('is read apart from the snapshot: a block that cannot be read leaves the snapshot whole; none from an old server is none', () => {
    const broken = viewWith({ status: 'ok' })
    expect(broken.nodes.length).toBeGreaterThan(0)
    expect(broken.explain).toMatchObject({ ok: false })
    expect(said(broken)).toMatch(/^Reasons unknown\. The explanation could not be read \(.+\)\. This does not mean nothing is in the way\.$/)
    expect('explain' in splitPlanExplain(r5Raw(), 'R5')).toBe(false)
    expect(why(viewWith(undefined))).toBeNull()
  })
})

describe('why a step is not done (§6, Design v3.15 §4.2)', () => {
  it('says first whether the read can be used: pending, failed, a step failure (before age and version), no result', () => {
    expect(said(viewWith(pending))).toBe('Reasons pending. The read has not answered yet.')
    expect(why(viewWith(failed))).toMatchObject({ word: 'Reasons unknown.', failed: true, list: null })
    expect(said(viewWith(ok(null)))).toMatch(/^No result\. The \d\d:\d\d read has no result for this step\.$/)
    expect(said(viewWith(ok({ kind: 'incomplete', reason: 'timeout' })))).toBe('Reasons incomplete. The read stopped for this step: it took too long. What it waits on is Unknown.')
    // A step's failure is said before the read's age and its version (review round 11 M6): no note beside it.
    const late = why(viewWith(ok({ kind: 'incomplete', reason: 'input_too_large', which: 'rows' }, new Date(NOW - 91_000).toISOString())))!
    expect([late.word, late.reason]).toEqual(['Reasons incomplete.', 'The read stopped for this step: too much to read (rows). What it waits on is Unknown.'])
    const err = why(viewWith({ ...ok(null), steps: { [STEP]: { node_version: version() + 1, kind: 'explain_error', code: 'read_failed' } } }))!
    expect([err.word, err.reason]).toEqual(['Reasons unknown.', 'The read for this step failed (read_failed). This does not mean nothing is in the way.'])
    // The plan view itself not read now: nothing is said (the step reads Unknown now already).
    expect(why(viewWith(ok({ kind: 'no_done_rule' })), 'old')).toBeNull()
  })

  it('M1 · concludes nothing about now from a read over 90 s old or of another step version, either way; keeps its list as that read\'s', () => {
    expect(why(viewWith(explained([pendingLanding()])))!.confirmed).toBe(true)
    const noTask = { atom: 'step_tasks', state: 'no_task' }
    const late = why(viewWith(explained([noTask], 'false', new Date(NOW - 120_000).toISOString())))!
    expect(late).toMatchObject({ word: 'Unconfirmed.', confirmed: false, version: version(), list: { rows: 1 } })
    expect(late.reason).toMatch(new RegExp(`^Reasons are from the \\d\\d:\\d\\d read of version ${version()}, 2 min ago \\(older than 90 s\\), so what is in the way now is not confirmed\\. Listed below as that read had it\\.$`))
    // The step moved on after the read: even a rule that held then says nothing of now.
    const atFive = (raw: ReturnType<typeof r5Raw>) => { raw.nodes.find(n => n.plan_node_id === STEP)!.version = 5 }
    const behind = why(viewWith({ ...ok(null), steps: { [STEP]: { node_version: 4, kind: 'explained', rule: 'true', blockers: [] } } }, atFive))!
    expect(behind.word).toBe('Unconfirmed.')
    expect(behind.reason).toMatch(/of version 4; the step changed since \(now version 5\), so what is in the way now is not confirmed\. That read said: Done by its rule\./)
    // The read is of a newer version than this plan view: the plan view is the older read.
    const ahead = why(viewWith({ ...ok(null), steps: { [STEP]: { node_version: version() + 1, kind: 'explained', rule: 'false', blockers: [noTask] } } }))!
    expect(ahead).toMatchObject({ word: 'Unconfirmed.', confirmed: false, version: version() + 1, stepVersion: version() })
    expect(ahead.reason).toMatch(new RegExp(`this plan view still holds version ${version()}, older than that read`))
    const rule = why(viewWith({ ...ok(null), steps: { [STEP]: { node_version: version() + 1, kind: 'no_done_rule' } } }))!
    expect([rule.word, rule.reason]).toEqual(['Unconfirmed.', expect.stringMatching(/That read said: No done rule\. Nothing can be checked/)])
    for (const w of [late, behind, ahead, rule]) expect(w.word).not.toMatch(/Moving|Nobody|Done by its rule/)
  })

  it('names each other result by its own word: held at this read, all left out, nothing named, no rule, not explained, not reached', () => {
    expect(said(viewWith(explained([], 'true')))).toBe('Done by its rule. Its done rule held at this read; the controller has not closed the step yet.')
    // Every blocker left out (over the 50 items): said so, never as nothing in the way (review round 11 M2).
    const all = why(viewWith(ok({ kind: 'explained', rule: 'false', blockers: [], omitted: 51 })))!
    expect([all.word, all.reason, all.list]).toEqual(['Unconfirmed.', '51 things stand in the way, none listed by the server (a step lists at most 50 items).', null])
    expect(said(viewWith(explained([])))).toBe('Unknown. Its done rule reads false, but the read lists nothing that holds it.')
    expect(said(viewWith(ok({ kind: 'no_done_rule' })))).toBe('No done rule. Nothing can be checked until one is set on the step.')
    expect(said(viewWith(ok({ kind: 'rule_not_supported', atoms: ['owner_signoff', 'manual'] })))).toBe('Not explained here. Its rule uses owner_signoff, manual, which this page does not explain.')
    expect(said(viewWith(ok({ kind: 'not_explained', reason: 'over_cap' })))).toMatch(/^Not reached\. The read explains 20 open steps at a time/)
  })

  it('concludes by §4.2\'s order: moving, then nobody, then unconfirmed, then a rule read unknown, then waits', () => {
    const moving = { stream: 'landing:bifrost-platform', operation: OP, phase: 'branch_pushed', outcome: '', reason: '', attempt: 'a_1', result: 'r', main_commit: '',
      run: '', observed_at: new Date(NOW - 60_000).toISOString(), basis: 'note.landing' }
    const onT3 = (raw: ReturnType<typeof r5Raw>) => { raw.tasks.find(t => t.task_id === T3)!.landing = moving }
    const noTask = { atom: 'step_tasks', state: 'no_task' }
    expect(why(viewWith(explained([pendingLanding(), noTask]), onT3))!.word).toBe('Moving.')
    expect(why(viewWith(explained([pendingLanding(), noTask])))!.word).toBe('Nobody is on it.')
    expect(why(viewWith(explained([pendingLanding()])))!.word).toBe('Unconfirmed.') // the plan view holds no landing for it
    const grant = pendingLanding({ phase: 'awaiting_grant', grant: { approval_id: 'appr_9', number: 281, last_status: 'pending', recorded_at: AT } })
    const waitingGrant = (raw: ReturnType<typeof r5Raw>) => { raw.tasks.find(t => t.task_id === T3)!.landing = { ...moving, phase: 'awaiting_grant' } }
    expect(why(viewWith(explained([grant], 'unknown'), waitingGrant))!.word).toBe('Unknown.')
    const w = why(viewWith(explained([grant]), waitingGrant))!
    expect(w.word).toBe('Waits.')
    expect(w.reason).toBe('Its landing is at awaiting_grant with request #281.')
  })

  it('tells a task candidate by the fresh snapshot: queued, or changed since the read; the name is its link, once', () => {
    const tasks = { atom: 'step_tasks', state: 'tasks_short', have: 1, need: 2, candidates: [{ task_id: T3, task_state: 'ready', since: AT }] }
    const v = viewWith(explained([tasks]))
    expect(rows(v).map(r => [r.kind, r.what, r.record, r.push.word])).toEqual([['Tasks', '1 of 2 accepted', 'Short', ''], ['Task', 'Module detail page', 'Ready', 'Queued']])
    expect(rows(v)[0].sub).toBe('1 not accepted, listed below')
    expect(rows(v)[1]).toMatchObject({ href: goalHref('M-251', T3), sub: T3, since: AT, child: true })
    expect(why(v)!.tasks).toEqual([T3])
    const accepted = (raw: ReturnType<typeof r5Raw>) => { raw.tasks.find(t => t.task_id === T3)!.state = 'accepted' }
    expect(rows(viewWith(explained([tasks], 'false', EARLIER), accepted))[1].push).toEqual({ kind: 'changed', word: 'Changed since read' })
    // M7: a snapshot not later than the read proves no change after it, only that the two reads differ.
    expect(rows(viewWith(explained([tasks]), accepted))[1].push).toEqual({ kind: 'unconfirmed', word: 'The two reads differ' })
    expect(rows(viewWith(explained([tasks], 'false', at(1_000)), accepted))[1].push.word).toBe('The two reads differ')
    const more = viewWith(explained([{ ...tasks, omitted: 4 }]))
    expect(rows(more)[0].sub).toBe('5 not accepted, listed below · 4 not listed by the server')
  })

  it('reads a landing moving or quiet only for the same operation; another one is current state unconfirmed', () => {
    const observed = (at: string, operation = OP, phase = 'branch_pushed') => (raw: ReturnType<typeof r5Raw>) => {
      raw.tasks.find(t => t.task_id === T3)!.landing = { stream: 'landing:bifrost-platform', operation, phase, outcome: '', reason: '', attempt: 'a_1',
        result: 'r', main_commit: '', run: '', observed_at: at, basis: 'note.landing' }
    }
    expect(first(viewWith(explained([pendingLanding()]), observed(new Date(NOW - 60_000).toISOString()))).push).toEqual({ kind: 'moving', word: 'Moving · record 1 min ago' })
    expect(first(viewWith(explained([pendingLanding()]), observed(new Date(NOW - 31 * 60_000).toISOString()))).push).toEqual({ kind: 'quiet', word: 'Quiet · 31 min' })
    expect(first(viewWith(explained([pendingLanding()]), observed(new Date(NOW - 60_000).toISOString(), `land:${T3}:1`))).push.word).toBe('Current state unconfirmed')
    expect(first(viewWith(explained([pendingLanding()]))).push.word).toBe('Current state unconfirmed')
    // M7: changed since only from a later plan view whose landing record is later than the read; else the reads differ.
    expect(first(viewWith(explained([pendingLanding()], 'false', EARLIER), observed(at(-10_000), OP, 'landed'))).push.word).toBe('Changed since read')
    expect(first(viewWith(explained([pendingLanding()], 'false', EARLIER), observed(at(-90_000), OP, 'landed'))).push.word).toBe('The two reads differ')
    expect(first(viewWith(explained([pendingLanding()]), observed(at(-10_000), OP, 'landed'))).push.word).toBe('The two reads differ')
    const grant = pendingLanding({ phase: 'awaiting_grant', grant: { approval_id: 'appr_9', number: 281, last_status: 'pending', recorded_at: AT } })
    const g = first(viewWith(explained([grant])))
    expect(g).toMatchObject({ kind: 'Landing', what: 'Module detail page', sub: `${OP} · phase awaiting_grant`, record: 'Pending', push: { kind: 'none' },
      request: { text: 'Request #281 · pending', href: approvalHref('appr_9') } })
  })

  it('says a card by its record only: bound (a link), closed, asking started, or prepared, in that order', () => {
    const cardOf = (c: Rec) => rows(viewWith(explained([{ atom: 'step_landed', state: 'landing_failed', task_id: T3, op: OP, outcome: 'stale', cards: [card1(c)] }])))[1]
    expect(cardOf({ question_id: 'q_0123456789abcdef', ask_started: true })).toMatchObject({ kind: 'Card', what: 'Question q_0123456789abcdef', href: questionHref('q_0123456789abcdef'), sub: 'root_failed:7:1' })
    expect(cardOf({ ask_started: true, closed: true, closed_why: 'resolved' })).toMatchObject({ what: 'closed (resolved)', sub: 'root_failed:7:1 · question number empty' })
    expect(cardOf({ closed: true })).toMatchObject({ what: 'closed (no reason recorded)' })
    expect(cardOf({ ask_started: true })).toMatchObject({ what: 'asked, number not recorded yet' })
    expect(cardOf({})).toMatchObject({ what: 'prepared, not asked yet' })
    // An open card: the landing waits on it, neither moving nor quiet; a closed one leaves it to the landing's own reading.
    const failedWith = (c: Rec) => first(viewWith(explained([{ atom: 'step_landed', state: 'landing_failed', task_id: T3, op: OP, outcome: 'stale', cards: [card1(c)], since: AT }])))
    expect(failedWith({ ask_started: true }).push.kind).toBe('none')
    expect(failedWith({ closed: true }).push.word).toBe('Current state unconfirmed')
    expect(failedWith({ closed: true }).since).toBe(AT)
  })

  it('says a declined root by its rework records: none, an earlier set, too many to ask, recorded, its card and outcome', () => {
    const rw = (rework: Rec) => rows(viewWith(explained([{ atom: 'step_landed', state: 'landing_declined', task_id: T3, op: OP, rework }])))
    const seen = { set_key: 'k', trigger_seq: 4, current: true }
    expect(rw({ seen: null, not_asked: false, card: null })[0].sub).toBe(`${OP} · no rework record`)
    expect(rw({ seen: { ...seen, current: false }, not_asked: false, card: null })[0].sub).toBe(`${OP} · rework record for an earlier set of landed tasks`)
    expect(rw({ seen, not_asked: true, card: null })[0].sub).toBe(`${OP} · reworked tasks too many to name on one card`)
    expect(rw({ seen, not_asked: false, card: null })[0].sub).toBe(`${OP} · rework recorded, no card yet`)
    expect(rw({ seen, not_asked: false, card: { n: 1, prep: 'rework_landed:4:1', question_id: null, ask_started: true, closed: true, closed_why: 'resolved', outcome: null } })[1])
      .toMatchObject({ kind: 'Card', what: 'closed (resolved)' })
    expect(rw({ seen, not_asked: false, card: { n: 1, prep: 'rework_landed:4:1', question_id: 'q_0123456789abcdef', ask_started: true, closed: false, closed_why: null,
      outcome: { outcome: 'still_owed', why: '', seq: 9 } } })[1]).toMatchObject({ what: 'Question q_0123456789abcdef', record: 'Outcome still_owed' })
    // No rework record: nobody is on it; reworked tasks landed in its place: what is left is its card.
    const v = (rework: Rec) => why(viewWith(explained([{ atom: 'step_landed', state: 'landing_declined', task_id: T3, op: OP, rework }])))!.word
    expect(v({ seen: null, not_asked: false, card: null })).toBe('Nobody is on it.')
    expect(v({ seen, not_asked: false, card: { n: 1, prep: 'p', question_id: 'q_0123456789abcdef', ask_started: true, closed: false, closed_why: null, outcome: null } })).toBe('Waits.')
  })

  it('says B5 by its records: any one of two paths, a repository block holding every new batch with its card, what is not explained', () => {
    const s = (staleOk as { steps: Record<string, { blockers: unknown[] }> }).steps['R5:5.5-10']
    const w = why(viewWith(explained(s.blockers, 'unknown')))!
    expect(w.list!.entries.map(e => e.kind)).toEqual(['any', 'row', 'row', 'row'])
    const any = w.list!.entries[0]
    if (any.kind !== 'any') throw new Error('one_of')
    expect(any.paths.map(p => flatRows(p).map(r => r.kind))).toEqual([['Tasks', 'Task'], ['Landing']])
    expect(flatRows(any.paths[1])[0].request).toEqual({ text: 'Request #281 · pending', href: approvalHref('appr_1') })
    const [stg, block, c] = flatRows(w.list!.entries.slice(1))
    expect(stg).toMatchObject({ kind: 'STG', sub: 'land:t_01M6HAMB152ZEPTPSZB0Z2KKNE:1 · never admitted · budget, policy, repo_cards not explained here', record: 'Not admitted' })
    expect(block).toMatchObject({ kind: 'Block', what: 'ci_red', sub: 'block ci-7 · holds every new batch of the repository', since: '2026-11-03T20:00:00Z', record: 'Open' })
    expect(c).toMatchObject({ kind: 'Card', what: 'closed (resolved)' })
    expect(w.reason).toBe('Neither of its 2 paths is open yet; STG has not admitted it (ci_red).')
  })

  it('M2 · keeps "any one of" inside a path as its own group, at every depth', () => {
    const noTask = { atom: 'step_tasks', state: 'no_task' }, noImpl = { atom: 'step_landed', state: 'no_implementation' }
    const w = why(viewWith(explained([{ atom: 'one_of', branches: [[{ atom: 'one_of', branches: [[noTask], [noImpl]] }], [noTask]] }])))!
    const outer = w.list!.entries[0]
    if (outer.kind !== 'any') throw new Error('outer group')
    const inner = outer.paths[0][0]
    expect(inner.kind).toBe('any')
    if (inner.kind !== 'any') return
    expect(inner.paths.map(p => flatRows(p).map(r => r.what))).toEqual([['No task yet'], ['No implementation task']])
    expect(flatRows(outer.paths[1]).map(r => r.what)).toEqual(['No task yet'])
    expect(w.list!.rows).toBe(3)
  })

  it('M3 · lists a block the server names for the member and the repository once, as holding every new batch', () => {
    const card = card1({ prep: 'stg_blocked:9:1', kind: 'stg_blocked', family: 'stg', ask_started: true })
    const block = { id: 'block-1', kind: 'ci_red', since: AT, trigger_seq: 9, cards: [card] }
    const w = why(viewWith(explained([{ atom: 'step_stg_settled', state: 'stg_not_admitted', task_id: T3, op: OP, to: null,
      blocks: [block], repo_blocks: [block], repo_blocks_omitted: 2, unexplained: [] }])))!
    expect(flatRows(w.list!.entries).map(r => r.kind)).toEqual(['STG', 'Block', 'Card'])
    expect(flatRows(w.list!.entries)[1].sub).toBe('block block-1 · holds every new batch of the repository · 2 more open blocks not listed by the server')
    expect(w.list).toMatchObject({ rows: 3, omitted: 2 })
  })

  it('M4 · counts every record the server left out, at every level, once for the heading, the last line and "N more"', () => {
    const tasks = { atom: 'step_tasks', state: 'tasks_short', have: 0, need: 1, candidates: [{ task_id: T3, task_state: 'blocked' }], omitted: 4 }
    const failedLanding = { atom: 'step_landed', state: 'landing_failed', task_id: T3, op: OP, outcome: 'stale', cards: [card1()], cards_omitted: 2 }
    const w = why(viewWith(ok({ kind: 'explained', rule: 'false', blockers: [tasks, failedLanding], omitted: 1 })))!
    // Rows: the tasks line, its candidate, the landing, its card; not listed: 4 candidates, 2 cards, 1 blocker.
    expect(w.list).toMatchObject({ rows: 4, omitted: 7 })
    expect(w.reason).toBe('0 of 1 tasks are accepted; a landing failed (stale); 1 more in the way.')
    // A candidate with no since from the server shows none.
    expect(flatRows(w.list!.entries)[1].since).toBeNull()
  })

  it('M8 · reads an STG batch moving only from the same operation in an STG phase, recorded since the batch\'s try', () => {
    const snapshot = at(-20 * 60_000)
    const batch = { atom: 'step_stg_settled', state: 'stg_batch_open', task_id: T3, op: OP, batch_id: 'stg-b-1', batch_state: 'OPEN',
      try: { n: 1, snapshot_at: snapshot, started_at: at(-19 * 60_000), bound_uid: null, run: null }, quarantine: null, blocks: [] }
    const landing = (phase: string, observed: string, operation = OP) => (raw: ReturnType<typeof r5Raw>) => {
      raw.tasks.find(t => t.task_id === T3)!.landing = { stream: 'landing:bifrost-platform', operation, phase, outcome: '', reason: '', attempt: 'a_1',
        result: 'r', main_commit: '', run: '', observed_at: observed, basis: 'note.landing' }
    }
    const push = (more: (raw: ReturnType<typeof r5Raw>) => void) => first(viewWith(explained([batch]), more)).push
    expect(push(landing('branch_pushed', at(-60_000)))).toEqual({ kind: 'unconfirmed', word: 'Current state unconfirmed' })
    expect(push(landing('stg_started', at(-60_000)))).toEqual({ kind: 'moving', word: 'Moving · record 1 min ago' })
    expect(push(landing('stg_started', at(-25 * 60_000))).kind).toBe('unconfirmed') // recorded before this try
    expect(push(landing('stg_started', at(-60_000), `land:${T3}:1`)).kind).toBe('unconfirmed') // another operation
  })

  it('never says whose turn it is', () => {
    const blockers = [pendingLanding(), { atom: 'step_stg_settled', state: 'stg_needs_landing' }, { atom: 'step_tasks', state: 'no_task' }]
    const s = (staleOk as { steps: Record<string, { blockers: unknown[] }> }).steps['R5:5.5-10']
    const d = (declined as unknown as { steps: Record<string, { blockers: unknown[] }> }).steps['R5:5.5-10']
    for (const sample of [explained(blockers), explained(s.blockers, 'unknown'), explained(d.blockers), pending, failed]) {
      expect(allWords(why(viewWith(sample))!)).not.toMatch(/waits? on you|your turn|for you|handled|已处理|等你/i)
    }
  })
})
