import type { ExplainBlock, ExplainBlocker, ExplainCard, ExplainRework, StepExplain } from '@/api/planExplainSchema'
import type { PlanNode, PlanSnapshotRead } from '@/api/planSnapshot'
import { approvalHref, goalHref, questionHref } from '@/lib/shell/consoleRoutes'
import { landingMoving, landingQuiet, LANDING_QUIET_MS, runningAttempt } from './liveStepModel'
import { heartbeatUnconfirmed } from './needsCleanupModel'

/*
 * Work → Plan: why a step in progress is not done (R5:5.5-19, DESIGN-r5-5.5-19-waiting-chain §6). The server names what
 * keeps the step's done rule from true, by record id only; this page says what each record is, links to it, and reads
 * the live part (an attempt, a landing moving) from its own fresh snapshot by the same id. It never says whose turn it
 * is: Needs you and Waiting on you say that, unchanged. Each line is from the records as read at the explanation's
 * read_at, which the line names.
 */

/** A link a line carries. */
export type WhyLink = { href: string; text: string }
/**
 * One line: its words, its links, and what it is: the result as a whole (state), one blocker, or how many blockers are
 * not listed here (omitted, with count): a view that shows fewer lines counts blockers by kind, never by lines.
 */
export type WhyLine = { text: string; links: WhyLink[]; kind: 'state' | 'blocker' | 'omitted'; count?: number }

/** An explanation older than this is not read now (§6 item 2). */
export const EXPLAIN_OLD_MS = 90_000

const hhmm = (iso: string) => {
  const d = new Date(iso)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}
const line = (text: string, links: WhyLink[] = []): WhyLine => ({ text, links, kind: 'blocker' })
const stateLine = (text: string): WhyLine => ({ text, links: [], kind: 'state' })
const notDone = (why: string): WhyLine => stateLine(`Not done because: ${why}.`)

/** What a result says before its blockers, or null when it has blockers to say. */
function resultWords(s: StepExplain): string | null {
  switch (s.kind) {
    case 'explained':
      if (s.blockers.length > 0) return null
      if (s.omitted) return `${s.omitted} ${s.omitted === 1 ? 'thing holds' : 'things hold'} it, not listed here (a step lists at most 50 items)`
      return s.rule === 'true' ? 'its done rule held at this read; the step\'s record is still In progress'
        : `its done rule reads ${s.rule} and the server names nothing that holds it`
    case 'no_done_rule': return 'this step has no done rule'
    case 'rule_not_supported': return `its done rule has conditions this page does not explain: ${s.atoms.join(', ')}`
    case 'not_explained': return 'this page explains 20 steps at a time; this one was not reached'
    case 'not_started': return 'not started'
    case 'incomplete': return `not computed (${s.reason === 'timeout' ? 'it took too long' : `too much to read${s.which ? `: ${s.which}` : ''}`})`
    case 'explain_error': return `not computed (${s.code})`
  }
}

/**
 * Why step is not done, line by line: the first blocker (all of them when all), each with its links; [] when the page
 * says nothing (no explain block from this server, the plan view itself not read now, a step not in progress).
 */
export function whyLines(view: PlanSnapshotRead, step: PlanNode, now: number, old: string, all = false): WhyLine[] {
  if (step.state !== 'active' || old || !view.explain) return []
  if (!view.explain.ok) return [notDone('not computed (the explanation could not be read)')]
  const ex = view.explain.explain
  if (ex.status === 'pending') return [notDone('not computed yet')]
  if (ex.status === 'failed' || !ex.read_at) return [notDone(`not computed (${ex.reason ?? 'error'})`)]
  const s = ex.steps[step.plan_node_id]
  // A step whose computation failed says so before the read's age or version (§6 item 2: failure first, then time).
  if (s && (s.kind === 'incomplete' || s.kind === 'explain_error')) return [notDone(resultWords(s) ?? 'not computed')]
  if (now - Date.parse(ex.read_at) > EXPLAIN_OLD_MS) return [notDone(`not read now (last read ${hhmm(ex.read_at)})`)]
  if (!s) return [notDone('not computed for this step')]
  if (s.node_version !== step.version) return [notDone('not read now (the step changed since it was read)')]
  const lead = `Not done because (read ${hhmm(ex.read_at)}): `
  const words = resultWords(s)
  if (words !== null || s.kind !== 'explained') return [stateLine(`${lead}${words}.`)]
  const said = (all ? s.blockers : s.blockers.slice(0, 1)).map(b => blockerLine(view, b, now))
  const more = s.blockers.length - said.length + (s.omitted ?? 0)
  const out = said.map((w, i) => line(i === 0 ? `${lead}${w.text}` : w.text, w.links))
  if (more > 0) out.push({ text: `+${more} more${s.omitted ? ` (${s.omitted} not listed by the server)` : ''}.`, links: [], kind: 'omitted', count: more })
  return out
}

/** A task by name, with its link when the snapshot holds it. */
function taskRef(view: PlanSnapshotRead, id: string): { name: string; links: WhyLink[] } {
  const t = view.tasks.find(x => x.task_id === id)
  const name = `task ${t?.title || id}`
  return { name, links: t ? [{ href: goalHref(t.mission_id, t.task_id), text: name }] : [] }
}

/** A card as its record says, never more: bound, being asked, closed, or prepared only (§6 item 5). */
function cardWords(c: Pick<ExplainCard, 'question_id' | 'prep' | 'ask_started' | 'closed' | 'closed_why'>): WhyLine {
  if (c.question_id) return line(`question card ${c.prep}`, [{ href: questionHref(c.question_id), text: `question ${c.question_id}` }])
  if (c.closed) return line(`card ${c.prep} closed (${c.closed_why ?? 'no reason recorded'}), no question id recorded`)
  if (c.ask_started) return line(`card ${c.prep}: asking started, no question id recorded yet (whether it was sent is not confirmed)`)
  return line(`card ${c.prep} prepared, not asked yet`)
}

function cardsWords(cards: ExplainCard[], omitted?: number): WhyLine | null {
  if (cards.length === 0) return null
  const ws = cards.map(cardWords)
  return line(`${ws.map(w => w.text).join('; ')}${omitted ? ` (+${omitted} older)` : ''}`, ws.flatMap(w => w.links))
}

function blocksWords(blocks: ExplainBlock[], omitted: number | undefined, lead: string): WhyLine | null {
  if (blocks.length === 0) return null
  const ws = blocks.map(b => {
    const c = cardsWords(b.cards, b.cards_omitted)
    return line(`${b.kind} since ${hhmm(b.since)}${c ? ` (${c.text})` : ', no card yet'}`, c?.links ?? [])
  })
  return line(`${lead}: ${ws.map(w => w.text).join('; ')}${omitted ? `; +${omitted} more` : ''}`, ws.flatMap(w => w.links))
}

function reworkWords(r: ExplainRework): WhyLine {
  if (!r.seen) return line('no rework record')
  if (!r.seen.current) return line('its rework record is for an earlier set of landed tasks on the step')
  if (r.not_asked) return line('its reworked tasks are too many to name on one card, so none is asked')
  if (!r.card) return line('its reworked tasks are recorded; no card yet')
  const c = cardWords(r.card)
  const out = r.card.outcome ? `; outcome recorded: ${r.card.outcome.outcome}${r.card.outcome.why ? ` (${r.card.outcome.why})` : ''}` : ''
  return line(`${c.text}${out}`, c.links)
}

/** Joins a lead and its parts into one line. */
function join(lead: string, links: WhyLink[], ...parts: (WhyLine | null)[]): WhyLine {
  const rest = parts.filter((p): p is WhyLine => p !== null)
  return line(`${[lead, ...rest.map(p => p.text)].join('; ')}.`, [...links, ...rest.flatMap(p => p.links)])
}

/** The base snapshot's landing of the task, when it is the same operation the explanation names. */
function sameLanding(view: PlanSnapshotRead, task: string, op: string) {
  const l = view.tasks.find(t => t.task_id === task)?.landing
  return l && l.operation === op ? l : null
}

/** How the base snapshot reads a landing the explanation names: moving or quiet only for the same operation. */
function landingNow(view: PlanSnapshotRead, task: string, op: string, now: number): string {
  const l = sameLanding(view, task, op)
  if (!l) return 'current state unconfirmed'
  if (l.phase === 'landed') return 'changed since: landed, awaiting refresh'
  if (!landingMoving(l)) return `now at phase ${l.phase}`
  return landingQuiet(l, now) ? `no landing record for over ${LANDING_QUIET_MS / 60_000} min` : 'moving'
}

/** One blocker in words. */
export function blockerLine(view: PlanSnapshotRead, b: ExplainBlocker, now: number): WhyLine {
  if (b.atom === 'one_of') {
    const branches = b.branches.map(br => br.map(x => blockerLine(view, x, now)))
    return line(`one of: ${branches.map((br, i) => `(${i + 1}) ${br.map(w => w.text.replace(/\.$/, '')).join('; ') || 'nothing named'}`).join(' or ')}.`,
      branches.flat().flatMap(w => w.links))
  }
  switch (b.state) {
    case 'tasks_short': {
      const cands = b.candidates.map(c => {
        const t = view.tasks.find(x => x.task_id === c.task_id)
        const ref = taskRef(view, c.task_id)
        if (t?.state === 'accepted') return line(`${ref.name} (changed since: accepted, awaiting refresh)`, ref.links)
        const a = t?.attempt
        const live = !t ? '' : a ? runningAttempt(t, view, now) ? ', its current attempt running' : heartbeatUnconfirmed(a, view, now)
          ? ', its current attempt unconfirmed' : `, its current attempt ${a.state}` : ', no live attempt'
        return line(`${ref.name} ${c.task_state}${live}`, ref.links)
      })
      const rest = b.omitted ? ` (+${b.omitted} more)` : ''
      return line(`${b.have} of ${b.need} tasks accepted${cands.length ? `; not yet: ${cands.map(c => c.text).join('; ')}${rest}` : ''}.`, cands.flatMap(c => c.links))
    }
    case 'no_task': return line('no task on this step yet.')
    case 'no_implementation': return line('no accepted implementation that writes code on this step.')
    case 'stg_needs_landing': return line('its STG verification waits until every implementation has landed.')
    case 'stg_not_supported': return line(`work in ${b.repo} has no STG release the runtime reads.`)
  }
  const t = taskRef(view, b.task_id)
  switch (b.state) {
    case 'no_landing_evidence': return line(`${t.name}: no landing recorded for it that the runtime reads.`, t.links)
    case 'landing_unclaimed': return line(`${t.name}: accepted${b.accepted_at ? ` ${hhmm(b.accepted_at)}` : ''}, its landing not claimed yet.`, t.links)
    case 'landing_pending': case 'landing_main_unsettled': {
      const what = b.state === 'landing_pending' ? `landing ${b.op} at phase ${b.phase}` : `landing ${b.op} asked main to move, main's answer not settled (phase ${b.phase})`
      const grant = b.grant ? line(`request #${b.grant.number} ${b.grant.last_status} as recorded ${hhmm(b.grant.recorded_at)}`,
        [{ href: approvalHref(b.grant.approval_id), text: `request #${b.grant.number}` }]) : null
      return join(`${t.name}: ${what}${b.last_record_at ? `, last record ${hhmm(b.last_record_at)}` : ''} (${landingNow(view, b.task_id, b.op, now)})`, t.links,
        grant, cardsWords(b.cards, b.cards_omitted))
    }
    case 'landing_failed': return join(`${t.name}: landing ${b.op} ended ${b.outcome}`, t.links, cardsWords(b.cards, b.cards_omitted))
    case 'landing_declined': return join(`${t.name}: landing ${b.op} declined`, t.links, reworkWords(b.rework))
    case 'landing_unmapped': return line(`${t.name}: landing ${b.op} at phase ${b.phase}${b.outcome ? ` (${b.outcome})` : ''}, a state this page does not name.`, t.links)
    case 'stg_withheld': return join(`${t.name}: withheld from STG (${b.kind})`, t.links, cardsWords(b.cards, b.cards_omitted), blocksWords(b.blocks, b.blocks_omitted, 'open blocks'))
    case 'stg_batch_open': {
      const tr = b.try ? `try ${b.try.n} snapshot ${hhmm(b.try.snapshot_at)}, ${b.try.started_at ? `started ${hhmm(b.try.started_at)}` : 'not started'}${b.try.bound_uid ? ', bound' : ''}${b.try.run ? `, run ${b.try.run}` : ''}` : 'no try yet'
      const q = b.quarantine ? join(`quarantined: ${b.quarantine.reason}`, [], cardsWords(b.quarantine.cards, b.quarantine.cards_omitted)) : null
      return join(`${t.name}: in STG batch ${b.batch_id} (${b.batch_state}), ${tr}`, t.links, q && line(q.text.replace(/\.$/, ''), q.links), blocksWords(b.blocks, b.blocks_omitted, 'open blocks'))
    }
    case 'stg_not_admitted': return join(`${t.name}: not in an STG batch (${b.to ?? 'never admitted'})`, t.links,
      blocksWords(b.blocks, b.blocks_omitted, 'open blocks on it'), blocksWords(b.repo_blocks, b.repo_blocks_omitted, 'the repository has open blocks, which hold every new batch'),
      line(`${b.unexplained.join(', ')} not explained here`))
    case 'stg_exhausted': return join(`${t.name}: STG tries exhausted`, t.links, cardsWords(b.cards, b.cards_omitted), blocksWords(b.blocks, b.blocks_omitted, 'open blocks'))
    case 'stg_disposed': return join(`${t.name}: disposed for STG`, t.links, blocksWords(b.blocks, b.blocks_omitted, 'open blocks'))
  }
}
