import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import declined from '@/api/__fixtures__/plan-explain-declined.json'
import failed from '@/api/__fixtures__/plan-explain-failed.json'
import pending from '@/api/__fixtures__/plan-explain-pending.json'
import staleOk from '@/api/__fixtures__/plan-explain-stale-ok.json'
import { parsePlanExplain, type ExplainBlocker } from '@/api/planExplainSchema'
import { splitPlanExplain, type PlanSnapshotRead } from '@/api/planSnapshot'
import { goalHref, questionHref } from '@/lib/shell/consoleRoutes'
import { blockerLine, whyLines } from '../explainModel'
import type { StepCell } from '../planModuleModel'
import { StepWhy } from '../StepWhy'
import { AT, r5Raw, T3 } from './moduleDetailFixture'

afterEach(cleanup)

/*
 * R5:5.5-19 (DESIGN-r5-5.5-19-waiting-chain §6, §7): why a step in progress is not done, from the plan view's explain
 * block. The four contract samples parse; each result and blocker says what its record is, links to it, and reads the
 * live part from the fresh snapshot by the same id; nothing says whose turn it is.
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
const explained = (blockers: Rec[], rule = 'false') => ok({ kind: 'explained', rule, blockers })
const lines = (v: PlanSnapshotRead, all = true) => whyLines(v, v.nodes.find(n => n.plan_node_id === STEP)!, NOW, '', all)
const text = (v: PlanSnapshotRead, all = true) => lines(v, all).map(l => l.text).join(' | ')
const card = (more: Rec = {}) => ({ question_id: null, prep: 'root_failed:7:1', kind: 'root_failed', family: 'landing', ask_started: false, closed: false, closed_why: null, ...more })
const pendingLanding = (more: Rec = {}) => ({ atom: 'step_landed', state: 'landing_pending', task_id: T3, op: OP, phase: 'branch_pushed',
  last_record_at: new Date(NOW - 60_000).toISOString(), grant: null, cards: [], ...more })

describe('the explain block (contract §7)', () => {
  it('reads the four samples: a declined root with its card, pending, failed cold, and an old success with a failure', () => {
    for (const sample of [declined, pending, failed, staleOk]) expect(parsePlanExplain(sample).ok).toBe(true)
    const d = parsePlanExplain(declined)
    if (!d.ok) throw new Error(d.error)
    const s = d.explain.steps['R5:5.5-10']
    expect(s.kind === 'explained' && s.blockers[0]).toMatchObject({ state: 'landing_declined', rework: { seen: { current: true }, card: { question_id: 'q_0123456789abcdef', outcome: null } } })
    const f = parsePlanExplain(failed)
    expect(f.ok && f.explain.status === 'failed' && f.explain.reason === 'input_too_large' && !f.explain.read_at && Object.keys(f.explain.steps)).toEqual([])
    const st = parsePlanExplain(staleOk)
    expect(st.ok && st.explain.last_error).toBe('timeout')
  })

  it('is closed: a key outside the contract, or a state it does not name, refuses the block; a phase word it does not know does not', () => {
    const bad = explained([{ atom: 'step_landed', state: 'landing_failed', task_id: T3, op: OP, outcome: 'stale', cards: [], extra: 1 }])
    expect(parsePlanExplain(bad).ok).toBe(false)
    expect(parsePlanExplain(explained([{ atom: 'step_landed', state: 'landing_somewhere', task_id: T3 }])).ok).toBe(false)
    expect(parsePlanExplain({ status: 'ok', steps: {} }).ok).toBe(false) // ok needs its read time
    expect(parsePlanExplain(explained([pendingLanding({ phase: 'a_phase_added_later' })])).ok).toBe(true)
  })

  it('is read apart from the snapshot: a block that cannot be read leaves the snapshot whole; none from an old server is none', () => {
    const broken = viewWith({ status: 'ok' })
    expect(broken.nodes.length).toBeGreaterThan(0)
    expect(broken.explain).toMatchObject({ ok: false })
    expect(text(broken)).toBe('Not done because: not computed (the explanation could not be read).')
    expect('explain' in splitPlanExplain(r5Raw(), 'R5')).toBe(false)
    expect(lines(viewWith(undefined))).toEqual([])
  })
})

describe('why a step is not done (§6)', () => {
  it('says first whether the explanation can be used: not computed yet, failed, old, the step changed, or no result for it', () => {
    expect(text(viewWith(pending))).toBe('Not done because: not computed yet.')
    expect(text(viewWith(failed))).toBe('Not done because: not computed (input_too_large).')
    expect(text(viewWith(ok({ kind: 'no_done_rule' }, new Date(NOW - 91_000).toISOString())))).toMatch(/^Not done because: not read now \(last read \d\d:\d\d\)\.$/)
    expect(text(viewWith({ ...ok(null), steps: { [STEP]: { node_version: version() + 1, kind: 'no_done_rule' } } }))).toBe('Not done because: not read now (the step changed since it was read).')
    expect(text(viewWith(ok(null)))).toBe('Not done because: not computed for this step.')
    expect(text(viewWith(ok({ kind: 'incomplete', reason: 'timeout' })))).toMatch(/not computed \(it took too long\)\.$/)
    // A step's failure is said before the read's age and its version (review round 11 M6).
    expect(text(viewWith(ok({ kind: 'incomplete', reason: 'timeout' }, new Date(NOW - 91_000).toISOString())))).toBe('Not done because: not computed (it took too long).')
    expect(text(viewWith({ ...ok(null), steps: { [STEP]: { node_version: version() + 1, kind: 'explain_error', code: 'read_failed' } } }))).toBe('Not done because: not computed (read_failed).')
    // The plan view itself not read now: nothing is said (the step reads Unknown now already).
    const v = viewWith(ok({ kind: 'no_done_rule' }))
    expect(whyLines(v, v.nodes.find(n => n.plan_node_id === STEP)!, NOW, 'old', true)).toEqual([])
  })

  it('names each result in words: held at this read, no rule, conditions not explained, not reached', () => {
    expect(text(viewWith(explained([], 'true')))).toMatch(/: its done rule held at this read; the step's record is still In progress\.$/)
    // Every blocker left out (over the 50 items): said so, never "nothing holds it" (review round 11 M2).
    expect(text(viewWith(ok({ kind: 'explained', rule: 'false', blockers: [], omitted: 51 })))).toMatch(/: 51 things hold it, not listed here \(a step lists at most 50 items\)\.$/)
    expect(text(viewWith(ok({ kind: 'no_done_rule' })))).toMatch(/: this step has no done rule\.$/)
    expect(text(viewWith(ok({ kind: 'rule_not_supported', atoms: ['owner_signoff', 'manual'] })))).toMatch(/does not explain: owner_signoff, manual\.$/)
    expect(text(viewWith(ok({ kind: 'not_explained', reason: 'over_cap' })))).toMatch(/explains 20 steps at a time; this one was not reached\.$/)
  })

  it('tells a task candidate by the fresh snapshot: its current attempt, or that it changed since the read', () => {
    const tasks = { atom: 'step_tasks', state: 'tasks_short', have: 1, need: 2, candidates: [{ task_id: T3, task_state: 'ready' }] }
    const v = viewWith(explained([tasks]))
    expect(text(v)).toMatch(/^Not done because \(read \d\d:\d\d\): 1 of 2 tasks accepted; not yet: task Module detail page ready, no live attempt\.$/)
    expect(lines(v)[0].links).toEqual([{ href: goalHref('M-251', T3), text: 'task Module detail page' }])
    const moved = viewWith(explained([tasks]), raw => { raw.tasks.find(t => t.task_id === T3)!.state = 'accepted' })
    expect(text(moved)).toMatch(/task Module detail page \(changed since: accepted, awaiting refresh\)/)
  })

  it('reads a landing moving or quiet only for the same operation; another one is current state unconfirmed', () => {
    const observed = (at: string, operation = OP) => (raw: ReturnType<typeof r5Raw>) => {
      raw.tasks.find(t => t.task_id === T3)!.landing = { stream: 'landing:bifrost-platform', operation, phase: 'branch_pushed', outcome: '', reason: '', attempt: 'a_1',
        result: 'r', main_commit: '', run: '', observed_at: at, basis: 'note.landing' }
    }
    expect(text(viewWith(explained([pendingLanding()]), observed(new Date(NOW - 60_000).toISOString())))).toMatch(/\(moving\)\.$/)
    expect(text(viewWith(explained([pendingLanding()]), observed(new Date(NOW - 31 * 60_000).toISOString())))).toMatch(/\(no landing record for over 30 min\)\.$/)
    expect(text(viewWith(explained([pendingLanding()]), observed(new Date(NOW - 60_000).toISOString(), `land:${T3}:1`)))).toMatch(/\(current state unconfirmed\)\.$/)
    expect(text(viewWith(explained([pendingLanding()])))).toMatch(/\(current state unconfirmed\)\.$/)
    const grant = pendingLanding({ phase: 'awaiting_grant', grant: { approval_id: 'appr_9', number: 281, last_status: 'pending', recorded_at: AT } })
    const l = lines(viewWith(explained([grant])))[0]
    expect(l.text).toMatch(/request #281 pending as recorded \d\d:\d\d/)
    expect(l.links.map(x => x.text)).toContain('request #281')
  })

  it('says a card by its record only: bound (a link), closed, asking started, or prepared, in that order', () => {
    const said = (c: Rec) => text(viewWith(explained([{ atom: 'step_landed', state: 'landing_failed', task_id: T3, op: OP, outcome: 'stale', cards: [card(c)] }])))
    expect(said({ question_id: 'q_0123456789abcdef', ask_started: true })).toMatch(/question card root_failed:7:1\.$/)
    expect(said({ ask_started: true, closed: true, closed_why: 'resolved' })).toMatch(/card root_failed:7:1 closed \(resolved\), no question id recorded\.$/)
    expect(said({ closed: true, closed_why: 'unconfirmed' })).toMatch(/closed \(unconfirmed\), no question id recorded\.$/)
    expect(said({ ask_started: true })).toMatch(/asking started, no question id recorded yet \(whether it was sent is not confirmed\)\.$/)
    expect(said({})).toMatch(/card root_failed:7:1 prepared, not asked yet\.$/)
    const bound = lines(viewWith(explained([{ atom: 'step_landed', state: 'landing_failed', task_id: T3, op: OP, outcome: 'stale', cards: [card({ question_id: 'q_0123456789abcdef' })] }])))[0]
    expect(bound.links).toContainEqual({ href: questionHref('q_0123456789abcdef'), text: 'question q_0123456789abcdef' })
  })

  it('says a declined root by its rework records: none, an earlier set, too many to ask, recorded, its card and outcome', () => {
    const rw = (rework: Rec) => text(viewWith(explained([{ atom: 'step_landed', state: 'landing_declined', task_id: T3, op: OP, rework }])))
    const seen = { set_key: 'k', trigger_seq: 4, current: true }
    expect(rw({ seen: null, not_asked: false, card: null })).toMatch(/declined; no rework record\.$/)
    expect(rw({ seen: { ...seen, current: false }, not_asked: false, card: null })).toMatch(/for an earlier set of landed tasks on the step\.$/)
    expect(rw({ seen, not_asked: true, card: null })).toMatch(/too many to name on one card, so none is asked\.$/)
    expect(rw({ seen, not_asked: false, card: null })).toMatch(/recorded; no card yet\.$/)
    expect(rw({ seen, not_asked: false, card: { n: 1, prep: 'rework_landed:4:1', question_id: null, ask_started: true, closed: true, closed_why: 'resolved', outcome: null } }))
      .toMatch(/card rework_landed:4:1 closed \(resolved\), no question id recorded\.$/)
    expect(rw({ seen, not_asked: false, card: { n: 1, prep: 'rework_landed:4:1', question_id: 'q_0123456789abcdef', ask_started: true, closed: false, closed_why: null,
      outcome: { outcome: 'still_owed', why: '', seq: 9 } } })).toMatch(/question card rework_landed:4:1; outcome recorded: still_owed\.$/)
  })

  it('says B5 by its records: a batch, a repository block holding every new batch with its card, and what is not explained', () => {
    const v = viewWith(staleOk, () => {})
    const s = (staleOk as { steps: Record<string, { blockers: ExplainBlocker[] }> }).steps['R5:5.5-10']
    const parsed = parsePlanExplain(staleOk)
    if (!parsed.ok) throw new Error(parsed.error)
    const b = (parsed.explain.steps['R5:5.5-10'] as { blockers: ExplainBlocker[] }).blockers
    expect(s.blockers).toHaveLength(2)
    const oneOf = blockerLine(v, b[0], NOW).text
    expect(oneOf).toMatch(/^one of: \(1\) 1 of 2 tasks accepted; .* or \(2\) task .*landing land:.*:2 at phase awaiting_grant.*request #281 pending/)
    const stg = blockerLine(v, b[1], NOW).text
    expect(stg).toMatch(/not in an STG batch \(never admitted\); the repository has open blocks, which hold every new batch: ci_red since \d\d:\d\d \(card stg_blocked:900:1 closed \(resolved\), no question id recorded\); budget, policy, repo_cards not explained here\.$/)
  })

  it('says the first blocker on the step and how many more, every one in the detail; and never whose turn it is', () => {
    const blockers = [pendingLanding(), { atom: 'step_stg_settled', state: 'stg_needs_landing' }, { atom: 'step_tasks', state: 'no_task' }]
    const first = lines(viewWith(explained(blockers)), false)
    expect(first.map(l => l.text)).toEqual([expect.stringMatching(/^Not done because \(read \d\d:\d\d\): task Module detail page: landing/), '+2 more.'])
    const all = lines(viewWith(explained(blockers)))
    expect(all).toHaveLength(3)
    for (const sample of [declined, staleOk, explained(blockers)]) {
      const v = viewWith(sample)
      const ex = v.explain?.ok ? v.explain.explain : null
      for (const step of Object.values(ex?.steps ?? {})) {
        if (step.kind !== 'explained') continue
        for (const b of step.blockers) expect(blockerLine(v, b, NOW).text).not.toMatch(/waits? on you|your turn|handled|已处理|等你/i)
      }
    }
  })
})

describe('StepWhy', () => {
  const cell = (kind: 'running' | 'idle'): StepCell => ({ id: STEP, label: '5.1-3', title: 't', state: kind === 'running' ? 'active' : 'idle', owner: false, effect: false,
    step: 'active', now: kind === 'running' ? { kind: 'running', what: 'Task running', others: 0 } : { kind: 'idle' }, noRule: false, since: null, rule: [], links: [], external: [],
    why: [{ text: 'Not done because (read 12:00): task x: landing y at phase branch_pushed.', links: [{ href: '#work?goal=M-1&task=t', text: 'task x' }], kind: 'blocker' }] } as StepCell)

  const two = (kind: 'running' | 'idle') => ({ ...cell(kind), why: [...cell(kind).why!, { text: 'second reason.', links: [], kind: 'blocker' }] }) as StepCell

  it('on the module page says the first reason and how many more; nothing while work runs (the detail says it)', () => {
    const { container } = render(<StepWhy cell={two('idle')} compact />)
    expect(container.querySelectorAll('[data-step-why-line]')).toHaveLength(1)
    expect(screen.getByText("+1 more in the step's detail.")).toBeInTheDocument()
    cleanup()
    const running = render(<StepWhy cell={two('running')} compact />)
    expect(running.container.querySelector('[data-step-why]')).toBeNull()
    cleanup()
    const full = render(<StepWhy cell={two('idle')} />)
    expect(full.container.querySelectorAll('[data-step-why-line]')).toHaveLength(2)
    expect(screen.queryByText(/more in the step's detail/)).toBeNull()
  })

  it('counts by kind on the module page: the detail\'s "+N more" line is no blocker (review round 12 S1)', () => {
    // One blocker shown by the server and 100 it did not list, as whyLines draws the detail.
    const v = viewWith(ok({ kind: 'explained', rule: 'false', omitted: 100, blockers: [pendingLanding()] }))
    const why = lines(v)
    expect(why.map(l => l.kind)).toEqual(['blocker', 'omitted'])
    const c = { ...cell('idle'), why } as StepCell
    const { container } = render(<StepWhy cell={c} compact />)
    expect(container.querySelectorAll('[data-step-why-line]')).toHaveLength(1)
    expect(container.querySelector('[data-step-why-more]')).toBeNull()
    expect(screen.getByText('100 more not listed (a step lists at most 50 items).')).toBeInTheDocument()
  })

  it('puts the reasons under what the step has now, with their links; under "Still needed for done" while work runs', () => {
    render(<StepWhy cell={cell('idle')} />)
    expect(screen.getByText(/Not done because \(read 12:00\)/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'task x' })).toHaveAttribute('href', '#work?goal=M-1&task=t')
    expect(screen.queryByText('Still needed for done')).toBeNull()
    cleanup()
    render(<StepWhy cell={cell('running')} />)
    expect(screen.getByText('Still needed for done')).toBeInTheDocument()
  })
})
