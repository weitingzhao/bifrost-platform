import type { AtomBlocker, ExplainBlock, ExplainBlocker, ExplainCard, ExplainRework, StepExplain } from '@/api/planExplainSchema'
import type { PlanNode, PlanSnapshotRead } from '@/api/planSnapshot'
import { approvalHref, goalHref, questionHref } from '@/lib/shell/consoleRoutes'
import { attemptRuns, landingMoving, landingQuiet } from './liveStepModel'
import { heartbeatUnconfirmed } from './needsCleanupModel'
import { sinceWords } from './readAge'

/*
 * Work → Plan: why a step in progress is not done (R5:5.5-19 contract §7; laid out as Design v3.15 ItemDetail Stages in
 * R5:5.5-20, API-REQUIREMENTS-IA-v2 §4.2). The server names what keeps the step's done rule from true, by record id
 * only. This page says, per record: what it is (a task by its name, the link, said once), its record word as the server
 * gives it, whether anyone is pushing it now, and since when; then one bold conclusion over all of them. Whether anyone
 * is pushing is the Console's own rules over its fresh plan view, by the same id, never a number of its own:
 * - a task: its current attempt's heartbeat (needsCleanupModel heartbeatUnconfirmed, 60 s, the Live tab's rule);
 *   queued as the step state reads it (stepStateModel judgeNow);
 * - a landing: moving or quiet (liveStepModel landingMoving / landingQuiet, 30 min) only while the plan view holds the
 *   same operation; else its current state is unconfirmed;
 * - an STG release: moving only from the same operation's landing in an STG phase, recorded since the batch's try;
 * - changed since the read only when the plan view is the later read and shows the record moved on; when it cannot
 *   show which read is later, the two reads differ, and nothing more is said.
 * Nothing here says whose turn it is: Needs you and Waiting on you say that, unchanged. A request a landing waits on is
 * named by its number and opens the same request Needs you opens; a record waiting on an open card or a request is
 * neither moving nor quiet.
 *
 * Counting, one rule for the list's heading, its last line and the conclusion's "N more", over the whole step: every
 * record listed is one row — a blocker, a candidate task, an STG block, a card — and each record once in the step, by
 * its identity (a block by id, a card by prep, a candidate by task id), under the first blocker that names it; a block
 * several tasks wait on says which (the server lists every open block of the repository for each member it explains:
 * stepexplain.go stgMember). Not listed is every record the server counted and left out, each one, and each set it
 * left them out of once: the step's blockers past its 50 items (omitted); candidates (tasks_short.omitted, by the set of
 * candidates); cards (cards_omitted, by the block or the card set they belong to); blocks: while any repository list
 * says how many open blocks it has (repo_blocks + repo_blocks_omitted: every one), that many less the distinct blocks
 * listed, else each member's own list once (blocks_omitted). In the way = rows + not listed.
 */

/** An explanation older than this is not read now: what it says is not confirmed (§6 item 2). */
export const EXPLAIN_OLD_MS = 90_000
/** A list shows its first SHOWN entries when it has more than SHOWN + 1 (Design v3.15: over 7, the first 6 and Show all). */
export const SHOWN = 6

/** Whether anyone is pushing one record now, judged by this page from the plan view by the record's id. */
export type PushKind = 'moving' | 'quiet' | 'unconfirmed' | 'changed' | 'queued' | 'none'
export type Push = { kind: PushKind; word: string }
/** What a row is: a task, the step's tasks counted, a landing, an STG release, an STG block, or a card. */
export type RowKind = 'Task' | 'Tasks' | 'Landing' | 'STG' | 'Block' | 'Card'
/**
 * One thing in the way: its kind, what it is (with its link when the plan view holds it), the ids that name it (task,
 * operation, batch or block), its record word, whether anyone is pushing it, since when (an RFC 3339 time, or null when
 * no record says), and child when it belongs to the row before it (a candidate to its tasks line; a block or card to
 * its blocker). request: the request a landing waits on, by its number, opening the same request Needs you opens.
 * task: the task whose name is the row's what.
 */
export type WhyRow = {
  kind: RowKind; what: string; href?: string; sub: string; record: string; push: Push; since: string | null; child: boolean
  request?: { text: string; href: string }; task?: string
}
/** A row, or "any one of" these paths, each its own entries (a path may hold another "any one of"). */
export type WhyEntry = { kind: 'row'; row: WhyRow } | { kind: 'any'; paths: WhyEntry[][] }
/**
 * Why a step is not done: the conclusion (a bold word and one reason; muted when it is about the read or the result as
 * a whole), the read's time, whether the read failed (the page may read again), confirmed (false when the read is older
 * than EXPLAIN_OLD_MS or of another version of the step: its list is then that read's), the step version the read is of
 * and the one the plan view holds, the list when the result names what is in the way (its entries, its rows, how many
 * records the server did not list), and the tasks the rows name (so the card names none twice).
 */
export type StepWhyModel = {
  word: string; reason: string; muted: boolean; readAt: string | null; failed: boolean
  confirmed: boolean; version: number | null; stepVersion: number
  list: { entries: WhyEntry[]; rows: number; omitted: number } | null
  tasks: string[]
}

const NONE: Push = { kind: 'none', word: '' }
const CHANGED: Push = { kind: 'changed', word: 'Changed since read' }
const DIFFER: Push = { kind: 'unconfirmed', word: 'The two reads differ' }
const UNCONFIRMED: Push = { kind: 'unconfirmed', word: 'Current state unconfirmed' }
const QUEUED: Push = { kind: 'queued', word: 'Queued' }

const pad = (n: number) => String(n).padStart(2, '0')
/** A record's time as the card says it: hh:mm on the day it is read, with the date on another day. */
export function clockWords(iso: string, now: number): string {
  const d = new Date(iso)
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  return d.toDateString() === new Date(now).toDateString() ? hm : `${d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })} ${hm}`
}
const cap = (s: string) => s ? `${s[0].toUpperCase()}${s.slice(1)}` : s
const quiet = (since: string | null, now: number): Push => ({ kind: 'quiet', word: since ? `Quiet · ${sinceWords(Date.parse(since), now)}` : 'Quiet' })

/** Task states a record word names; any other is said as the server gives it. */
const TASK_WORDS: Record<string, string> = {
  proposed: 'Proposed', awaiting_owner: 'Awaiting owner', ready: 'Ready', leased: 'Leased', running: 'Running', submitted: 'Submitted',
  verifying: 'Verifying', accepted: 'Accepted', rework: 'Rework', rejected: 'Rejected', blocked: 'Blocked', paused: 'Paused', cancelled: 'Cancelled',
}
/** Task states that wait their turn, as the step state reads them (stepStateModel QUEUED_TASK). */
const TASK_QUEUED = new Set(['proposed', 'ready', 'rework', 'submitted', 'verifying'])
/** The landing phases of a release to STG (landing/protocol.go; liveStepModel LANDING_NOW): only these show one moving. */
const STG_PHASES = new Set(['stg_pending', 'stg_snapshot', 'stg_started', 'stg_result'])
/** The record word of each blocker state (§7). */
const RECORD: Record<AtomBlocker['state'], string> = {
  tasks_short: 'Short', no_task: 'No task', no_implementation: 'Missing', no_landing_evidence: 'No record', landing_unclaimed: 'Not claimed',
  landing_pending: 'Pending', landing_main_unsettled: 'Main unsettled', landing_failed: 'Failed', landing_declined: 'Declined', landing_unmapped: 'Unmapped',
  stg_needs_landing: 'Needs landing', stg_not_supported: 'Not supported', stg_withheld: 'Withheld', stg_batch_open: 'Open', stg_not_admitted: 'Not admitted',
  stg_exhausted: 'Exhausted', stg_disposed: 'Disposed',
}

/**
 * What one explanation is read against: the plan view, now, the seconds since its snapshot, the rule's word for its
 * tasks, the read's time, and whether the plan view's snapshot is the later of the two reads.
 */
type Ctx = { view: PlanSnapshotRead; now: number; drift: number; tasksWord: string; readAt: number; later: boolean; listed?: Listed }
/**
 * What a step's list has shown so far, so that a record several blockers name is one row and a set the server left
 * records out of is counted once (see Counting above): ids, the records and sets already taken; byBlock, every task each
 * STG block is in the way of, read over the whole step first; repoBlocks, how many open blocks the repository has as a
 * repository list says, else null; memberUnlisted, the blocks members' own lists left out, each list once; blocks, the
 * distinct blocks listed. Without one (judging a blocker alone), every record is taken.
 */
type Listed = { ids: Set<string>; byBlock: Map<string, string[]>; repoBlocks: number | null; memberUnlisted: number; blocks: number }
/** Takes key the first time the list meets it; true when there is no list to share (a blocker judged alone). */
const first = (ctx: Ctx, key: string) => { const l = ctx.listed; if (!l) return true; if (l.ids.has(key)) return false; l.ids.add(key); return true }
/** A task id as a sub line names a few of them: its last six characters (t_…F0WBP3), a short one whole. */
const shortId = (id: string) => id.length > 12 ? `t_…${id.slice(-6)}` : id
/** The STG blocks of the whole step, by id, with each task they are in the way of, and the repository's count of open blocks. */
function collectBlocks(blockers: ExplainBlocker[], l: Listed) {
  for (const b of blockers) {
    if (b.atom === 'one_of') { b.branches.forEach(br => collectBlocks(br, l)); continue }
    if (!('blocks' in b)) continue
    for (const x of [...b.blocks, ...(b.state === 'stg_not_admitted' ? b.repo_blocks : [])]) {
      const tasks = l.byBlock.get(x.id) ?? []
      if (!tasks.includes(b.task_id)) l.byBlock.set(x.id, [...tasks, b.task_id])
    }
    if (b.state === 'stg_not_admitted') l.repoBlocks = Math.max(l.repoBlocks ?? 0, b.repo_blocks.length + (b.repo_blocks_omitted ?? 0))
  }
}
const taskOf = (ctx: Ctx, id: string) => ctx.view.tasks.find(t => t.task_id === id)
/** A task by its name, the link when the plan view holds it; its id when it does not. */
function taskWhat(ctx: Ctx, id: string): { what: string; href?: string; task: string } {
  const t = taskOf(ctx, id)
  return t ? { what: t.title || id, href: goalHref(t.mission_id, t.task_id), task: id } : { what: id, task: id }
}
/** A record the plan view shows different from the read: changed since only when the plan view is the later read. */
const moved = (ctx: Ctx): Push => ctx.later ? CHANGED : DIFFER
/** A landing the plan view shows different: changed since only when its own record there is later than the read. */
const landingMoved = (ctx: Ctx, l: { observed_at: string }): Push => ctx.later && Date.parse(l.observed_at) > ctx.readAt ? CHANGED : DIFFER

/** Whether anyone is pushing a task the explanation read in state said, from the plan view by its id. */
function taskPush(ctx: Ctx, id: string, said: string, since: string | null): Push {
  const t = taskOf(ctx, id)
  if (!t) return UNCONFIRMED
  if (t.state !== said) return moved(ctx)
  const a = t.attempt
  if (a) {
    const silent = `${Math.round(a.heartbeat_age_s + ctx.drift)} s`
    if (a.state === 'stale') return { kind: 'unconfirmed', word: 'Unconfirmed · attempt stale' }
    if (!attemptRuns(a.state)) return { kind: 'unconfirmed', word: `Unconfirmed · attempt ${a.state}` }
    return heartbeatUnconfirmed(a, ctx.view, ctx.now) ? { kind: 'unconfirmed', word: `Unconfirmed · silent ${silent}` } : { kind: 'moving', word: `Moving · heartbeat ${silent}` }
  }
  if (t.state === 'leased' || t.state === 'running') return { kind: 'unconfirmed', word: 'Unconfirmed · no live attempt' }
  if (TASK_QUEUED.has(t.state)) return QUEUED
  // Awaiting the Owner: Needs you lists it; nobody else pushes it, and it is not quiet.
  if (t.state === 'awaiting_owner') return NONE
  return quiet(since, ctx.now)
}

/** The plan view's landing of the task when it is the same operation the explanation names. */
function sameLanding(ctx: Ctx, task: string, op: string) {
  const l = taskOf(ctx, task)?.landing
  return l && l.operation === op ? l : null
}

/**
 * Whether anyone is pushing a landing the explanation names, from the plan view's landing of the same operation only:
 * moving with a record in the last 30 min, else quiet; parked is queued (the controller unparks it); awaiting the grant
 * waits on its request (neither). said: the phase the explanation read (a different one now is a change, or the two
 * reads differing), 'ended' for a landing it read as ended (moving or waiting again is one), or null to compare nothing.
 */
function landingPush(ctx: Ctx, task: string, op: string, said: string | null): Push {
  const l = sameLanding(ctx, task, op)
  if (!l) return UNCONFIRMED
  const moving = landingMoving(l)
  const differs = said === 'ended' ? moving || l.phase === 'parked' || l.phase === 'awaiting_grant'
    // replay_due is the explanation's own word for an operation that ended without ending its root.
    : said !== null && said !== 'replay_due' && l.phase !== said
  if (differs) return landingMoved(ctx, l)
  if (moving) return landingQuiet(l, ctx.now) ? quiet(l.observed_at, ctx.now) : { kind: 'moving', word: `Moving · record ${sinceWords(Date.parse(l.observed_at), ctx.now)} ago` }
  if (l.phase === 'parked') return QUEUED
  if (l.phase === 'awaiting_grant') return NONE
  return quiet(l.observed_at, ctx.now)
}

/** Some rows, and how many records of them the server counted and did not list. */
type Part = { rows: WhyRow[]; unlisted: number }
const part = (rows: WhyRow[], unlisted = 0): Part => ({ rows, unlisted })
const join = (...parts: Part[]): Part => ({ rows: parts.flatMap(p => p.rows), unlisted: parts.reduce((n, p) => n + p.unlisted, 0) })

/** A card as its record says, never more: bound (its question, the link), closed, being asked, or prepared only (§6 item 5). */
function cardRow(c: Pick<ExplainCard, 'question_id' | 'prep' | 'ask_started' | 'closed' | 'closed_why'>, more = ''): WhyRow {
  const base = { kind: 'Card' as const, record: '', push: NONE, since: null, child: true }
  if (c.question_id) return { ...base, what: `Question ${c.question_id}`, href: questionHref(c.question_id), sub: `${c.prep}${more}` }
  const what = c.closed ? `closed (${c.closed_why ?? 'no reason recorded'})` : c.ask_started ? 'asked, number not recorded yet' : 'prepared, not asked yet'
  return { ...base, what, sub: `${c.prep} · question number empty${more}` }
}
/**
 * A group of cards, each once in the step (by its prep, the card's identity), the last saying how many older ones the
 * server left out; those counted once for the set they belong to (set: a block's cards by its id, else the cards listed).
 */
function cardRows(ctx: Ctx, cards: ExplainCard[], omitted = 0, set = `cards ${cards.map(c => c.prep).sort().join(',')}`): Part {
  const shown = cards.filter((c, i) => cards.findIndex(x => x.prep === c.prep) === i && first(ctx, `card ${c.prep}`))
  const left = omitted && first(ctx, `left ${set}`) ? omitted : 0
  return part(shown.map((c, i) => cardRow(c, i === shown.length - 1 && left ? ` · ${left} older not listed by the server` : '')), left)
}
const openCard = (cards: Pick<ExplainCard, 'closed'>[]) => cards.some(c => !c.closed)

/**
 * STG blocks, each once in the step with its cards under it: a repository's own say that they hold every new batch, and
 * one several tasks wait on names them. The blocks the server left out are counted over the whole step (explained).
 */
function blockRows(ctx: Ctx, blocks: { block: ExplainBlock; repo: boolean }[]): Part {
  return join(...blocks.filter(({ block }) => first(ctx, `block ${block.id}`)).map(({ block: b, repo }) => {
    if (ctx.listed) ctx.listed.blocks++
    const tasks = ctx.listed?.byBlock.get(b.id) ?? []
    return join(part([{ kind: 'Block', what: b.kind, record: 'Open', push: NONE, since: b.since, child: true,
      sub: `block ${b.id}${repo ? ' · holds every new batch of the repository' : ''}${tasks.length > 1 ? ` · in the way of ${tasks.map(shortId).join(', ')}` : ''}` }]),
    cardRows(ctx, b.cards, b.cards_omitted, `block ${b.id}`))
  }))
}
/** A member's own blocks; the ones its list left out counted once for that list. */
function memberBlocks(ctx: Ctx, blocks: ExplainBlock[], omitted = 0): Part {
  if (omitted && ctx.listed && first(ctx, `left blocks ${blocks.map(b => b.id).sort().join(',')} ${omitted}`)) ctx.listed.memberUnlisted += omitted
  return blockRows(ctx, blocks.map(block => ({ block, repo: false })))
}
/** ", held by block x, listed above": the blocks of a blocker an earlier one already lists (read before its own are taken). */
function listedAbove(ctx: Ctx, blocks: ExplainBlock[]): string {
  const above = [...new Set(blocks.map(b => b.id))].filter(id => ctx.listed?.ids.has(`block ${id}`))
  return above.length ? ` · held by block ${above.join(', ')}, listed above` : ''
}
const earliest = (times: (string | null | undefined)[]) => times.filter((t): t is string => !!t).sort((a, b) => Date.parse(a) - Date.parse(b))[0] ?? null

/** A declined landing's rework records, as its sub line says them. */
function reworkWords(r: ExplainRework): string {
  if (!r.seen) return 'no rework record'
  if (!r.seen.current) return 'rework record for an earlier set of landed tasks'
  if (r.not_asked) return 'reworked tasks too many to name on one card'
  return r.card ? 'rework recorded' : 'rework recorded, no card yet'
}
/** A rework record for the step's landed tasks now: another task's change landed in its place. */
const reworked = (r: ExplainRework) => !!r.seen?.current

/** The rows of one blocker that is not one_of, and how many of its records the server did not list. */
function atomRows(ctx: Ctx, b: AtomBlocker): Part {
  const row = (r: Omit<WhyRow, 'child' | 'record'>): WhyRow => ({ record: RECORD[b.state], child: false, ...r })
  switch (b.state) {
    case 'tasks_short': {
      const not = b.candidates.length + (b.omitted ?? 0)
      // A candidate another tasks line already lists, and a set of candidates already counted, are not again.
      const shown = b.candidates.filter(c => first(ctx, `task ${c.task_id}`))
      const left = b.omitted && first(ctx, `left candidates ${b.candidates.map(c => c.task_id).sort().join(',')} ${b.omitted}`) ? b.omitted : 0
      return part([row({ kind: 'Tasks', what: `${b.have} of ${b.need} ${ctx.tasksWord}`, push: NONE, since: null,
        sub: `${not} not ${ctx.tasksWord}${shown.length ? ', listed below' : b.candidates.length ? ', listed above' : ''}${left ? ` · ${left} not listed by the server` : ''}` }),
        ...shown.map(c => ({ kind: 'Task' as const, ...taskWhat(ctx, c.task_id), sub: c.task_id, record: TASK_WORDS[c.task_state] ?? c.task_state,
          push: taskPush(ctx, c.task_id, c.task_state, c.since ?? null), since: c.since ?? null, child: true }))], left)
    }
    case 'no_task': return part([row({ kind: 'Task', what: 'No task yet', sub: 'nothing is assigned to this step', push: NONE, since: null })])
    case 'no_implementation': return part([row({ kind: 'Landing', what: 'No implementation task', sub: 'no accepted task that writes code is on this step', push: NONE, since: null })])
    case 'stg_needs_landing': return part([row({ kind: 'STG', what: 'Release to STG', sub: 'starts after every implementation has landed', push: NONE, since: null })])
    case 'stg_not_supported': return part([row({ kind: 'STG', what: 'Release to STG', sub: `repo ${b.repo} has no STG release the runtime reads`, push: NONE, since: null })])
  }
  const task = taskWhat(ctx, b.task_id)
  // A landing recorded on the task now: one began since the read, when its record there is later than the read.
  const begun = () => { const l = taskOf(ctx, b.task_id)?.landing; return l ? landingMoved(ctx, l) : null }
  switch (b.state) {
    case 'no_landing_evidence':
      return part([row({ kind: 'Landing', ...task, sub: `${b.task_id} · accepted, no landing record the runtime reads`, push: begun() ?? quiet(null, ctx.now), since: null })])
    case 'landing_unclaimed':
      return part([row({ kind: 'Landing', ...task, sub: `${b.task_id} · accepted, no landing has claimed it`, push: begun() ?? quiet(b.accepted_at, ctx.now), since: b.accepted_at })])
    case 'landing_pending': case 'landing_main_unsettled': {
      const grant = b.grant
      const read = landingPush(ctx, b.task_id, b.op, b.phase)
      // At the grant it waits on its request; with an open card, on that card: neither, unless it is seen moving or moved on.
      const push = (grant || openCard(b.cards)) && read.kind !== 'moving' && read.kind !== 'changed' && read !== DIFFER ? NONE : read
      const what = b.state === 'landing_pending' ? `phase ${b.phase}` : `main has not settled (phase ${b.phase})`
      return join(part([row({ kind: 'Landing', ...task, sub: `${b.op} · ${what}`, push, since: b.last_record_at,
        request: grant ? { text: `Request #${grant.number} · ${grant.last_status}`, href: approvalHref(grant.approval_id) } : undefined })]),
        cardRows(ctx, b.cards, b.cards_omitted))
    }
    case 'landing_failed': {
      const push = openCard(b.cards) ? NONE : landingPush(ctx, b.task_id, b.op, 'ended')
      return join(part([row({ kind: 'Landing', ...task, sub: `${b.op} · result ${b.outcome}`, push, since: b.since ?? sameLanding(ctx, b.task_id, b.op)?.observed_at ?? null })]),
        cardRows(ctx, b.cards, b.cards_omitted))
    }
    case 'landing_declined': {
      // Reworked tasks landed in its place: what is left is its card's, not quiet.
      const card = b.rework.card
      const push = reworked(b.rework) && !card?.closed ? NONE : landingPush(ctx, b.task_id, b.op, 'ended')
      const cards = card && first(ctx, `card ${card.prep}`) ? [{ ...cardRow(card), record: card.outcome ? `Outcome ${card.outcome.outcome}` : '' }] : []
      return part([row({ kind: 'Landing', ...task, sub: `${b.op} · ${reworkWords(b.rework)}`, push, since: b.since ?? sameLanding(ctx, b.task_id, b.op)?.observed_at ?? null }), ...cards])
    }
    case 'landing_unmapped': return part([row({ kind: 'Landing', ...task, sub: `${b.op} · phase ${b.phase}${b.outcome ? ` · ${b.outcome}` : ''}, which this page does not name`,
      push: landingPush(ctx, b.task_id, b.op, b.phase), since: sameLanding(ctx, b.task_id, b.op)?.observed_at ?? null })])
  }
  // STG: the plan view holds no batch. Its landing of the same operation in an STG phase, with a record in the last
  // 30 min, is all that can show a release moving, and only for an open batch whose try it is recorded since; a
  // release held by a block or a hold of its own is quiet; one waiting on an open card is neither.
  const l = sameLanding(ctx, b.task_id, b.op)
  const stgLive = l && STG_PHASES.has(l.phase) && landingMoving(l) && !landingQuiet(l, ctx.now) ? l : null
  // Outside an open batch, a release seen moving contradicts the read: moved on since it, or the reads differ.
  const contradicted = stgLive ? landingMoved(ctx, stgLive) : null
  const held = (cards: ExplainCard[], holds: boolean, since: string | null): Push => contradicted ?? (openCard(cards) ? NONE : holds ? quiet(since, ctx.now) : NONE)
  const cardsOf = (blocks: ExplainBlock[]) => blocks.flatMap(x => x.cards)
  switch (b.state) {
    case 'stg_withheld': {
      const since = earliest(b.blocks.map(x => x.since))
      return join(part([row({ kind: 'STG', ...task, sub: `${b.op} · withheld (${b.kind})${listedAbove(ctx, b.blocks)}`, push: held([...b.cards, ...cardsOf(b.blocks)], true, since), since })]),
        cardRows(ctx, b.cards, b.cards_omitted), memberBlocks(ctx, b.blocks, b.blocks_omitted))
    }
    case 'stg_batch_open': {
      const t = b.try, q = b.quarantine
      const tr = t ? ` · try ${t.n}${t.started_at ? '' : ', not started'}${t.run ? ` · run ${t.run}` : ''}` : ' · no try yet'
      const since = t?.started_at ?? t?.snapshot_at ?? null
      // Moving: the same operation, an STG phase, recorded since this try's snapshot. Quarantined: held, on its card
      // while one is open. Else the batch itself is not in the plan view: unconfirmed.
      const seen = stgLive && t && Date.parse(stgLive.observed_at) >= Date.parse(t.snapshot_at)
        ? { kind: 'moving' as const, word: `Moving · record ${sinceWords(Date.parse(stgLive.observed_at), ctx.now)} ago` } : null
      const push = seen ?? (q ? openCard([...q.cards, ...cardsOf(b.blocks)]) ? NONE : quiet(since, ctx.now) : UNCONFIRMED)
      return join(part([row({ kind: 'STG', ...task, sub: `batch ${b.batch_id} · ${b.batch_state}${tr}${q ? ` · quarantined (${q.reason})` : ''}${listedAbove(ctx, b.blocks)}`, push, since })]),
        q ? cardRows(ctx, q.cards, q.cards_omitted) : part([]), memberBlocks(ctx, b.blocks, b.blocks_omitted))
    }
    case 'stg_not_admitted': {
      // The member's blocks and the repository's are open blocks of one repository (stepexplain.go stgBlocks): one
      // block in both is one row, marked as holding every new batch (and once in the step: blockRows). The blocks left
      // out are counted over the whole step from the repository's count of them (explained).
      const byId = new Map<string, { block: ExplainBlock; repo: boolean }>()
      for (const x of b.blocks) if (!byId.has(x.id)) byId.set(x.id, { block: x, repo: false })
      for (const x of b.repo_blocks) { const e = byId.get(x.id); if (e) e.repo = true; else byId.set(x.id, { block: x, repo: true }) }
      const all = [...byId.values()]
      const since = earliest(all.map(x => x.block.since))
      const above = listedAbove(ctx, all.map(x => x.block))
      return join(part([row({ kind: 'STG', ...task, sub: `${b.op} · ${b.to ?? 'never admitted'}${b.unexplained.length ? ` · ${b.unexplained.join(', ')} not explained here` : ''}${above}`,
        push: held(cardsOf(all.map(x => x.block)), all.length > 0, since), since })]), blockRows(ctx, all))
    }
    case 'stg_exhausted': {
      const since = earliest(b.blocks.map(x => x.since))
      return join(part([row({ kind: 'STG', ...task, sub: `${b.op} · every STG try used${listedAbove(ctx, b.blocks)}`, push: held([...b.cards, ...cardsOf(b.blocks)], true, since), since })]),
        cardRows(ctx, b.cards, b.cards_omitted), memberBlocks(ctx, b.blocks, b.blocks_omitted))
    }
    case 'stg_disposed': {
      const since = earliest(b.blocks.map(x => x.since))
      return join(part([row({ kind: 'STG', ...task, sub: `${b.op} · disposed for STG${listedAbove(ctx, b.blocks)}`, push: held(cardsOf(b.blocks), true, since), since })]),
        memberBlocks(ctx, b.blocks, b.blocks_omitted))
    }
  }
}

/** The entries of some blockers: each one's rows; a one_of as its paths, each path its own entries, at every depth. */
function entriesOf(ctx: Ctx, blockers: ExplainBlocker[]): { entries: WhyEntry[]; unlisted: number } {
  const out = blockers.map((b): { entries: WhyEntry[]; unlisted: number } => {
    if (b.atom === 'one_of') {
      const paths = b.branches.map(br => entriesOf(ctx, br))
      return { entries: [{ kind: 'any', paths: paths.map(p => p.entries) }], unlisted: paths.reduce((n, p) => n + p.unlisted, 0) }
    }
    const p = atomRows(ctx, b)
    return { entries: p.rows.map(row => ({ kind: 'row', row })), unlisted: p.unlisted }
  })
  return { entries: out.flatMap(o => o.entries), unlisted: out.reduce((n, o) => n + o.unlisted, 0) }
}
/** Every row of some entries, the paths' too, at every depth (for counting; the entries keep their groups). */
export const flatRows = (entries: WhyEntry[]): WhyRow[] => entries.flatMap(e => e.kind === 'row' ? [e.row] : e.paths.flatMap(flatRows))

/*
 * The conclusion (R1, §4.2's table, the first that holds): Moving while anything in the way is moving; Nobody is on it
 * while something is quiet, or there is no task, no implementation, an unclaimed landing, or a declined one with no
 * rework landed in its place; Unconfirmed while something cannot be confirmed (or changed since the read); Unknown
 * while the rule itself reads unknown; else Waits (on another item, a request, a card, its turn). Never "waits on you":
 * that is Needs you's word. Any one of several paths is moving while one of them moves, and has nobody on it only when
 * none of them has anyone.
 */
type Sig = 'moving' | 'nobody' | 'unconfirmed' | 'waits'
const NOBODY_STATES: ReadonlySet<string> = new Set(['no_task', 'no_implementation', 'landing_unclaimed'])
/** Of all of these (a step's blockers, one path): moving first, then nobody, then unconfirmed. */
const allSig = (sigs: Sig[]): Sig => (['moving', 'nobody', 'unconfirmed'] as const).find(s => sigs.includes(s)) ?? 'waits'
/** Any one of these paths: moving while one moves; nobody only when every path has nobody on it. */
const anySig = (sigs: Sig[]): Sig => sigs.includes('moving') ? 'moving' : sigs.length > 0 && sigs.every(s => s === 'nobody') ? 'nobody'
  : sigs.includes('unconfirmed') ? 'unconfirmed' : 'waits'
function blockerSig(ctx: Ctx, b: ExplainBlocker): Sig {
  if (b.atom === 'one_of') return anySig(b.branches.map(br => allSig(br.map(x => blockerSig(ctx, x)))))
  const { rows } = atomRows(ctx, b)
  const kinds = new Set(rows.map(r => r.push.kind))
  const changed = rows[0]?.push.kind === 'changed' || rows[0]?.push === DIFFER
  if (kinds.has('moving')) return 'moving'
  if (kinds.has('quiet') || (!changed && (NOBODY_STATES.has(b.state) || (b.state === 'landing_declined' && !reworked(b.rework))))) return 'nobody'
  if (kinds.has('unconfirmed') || kinds.has('changed')) return 'unconfirmed'
  return 'waits'
}

/** The reason's phrase for one blocker (Design plan-explain-sample PH), never whose turn it is. */
function phrase(ctx: Ctx, b: ExplainBlocker): string {
  if (b.atom === 'one_of') return b.branches.length === 2 ? 'neither of its 2 paths is open yet' : `none of its ${b.branches.length} paths is open yet`
  switch (b.state) {
    case 'tasks_short': return `${b.have} of ${b.need} tasks are ${ctx.tasksWord}`
    case 'no_task': return 'no task exists for it yet'
    case 'no_implementation': return 'no implementation task is on it'
    case 'no_landing_evidence': return 'an accepted task has no landing record'
    case 'landing_unclaimed': return 'no landing has claimed its task yet'
    case 'landing_pending': return `its landing is at ${b.phase}${b.grant ? ` with request #${b.grant.number}` : ''}`
    case 'landing_main_unsettled': return 'main has not settled its landing'
    case 'landing_failed': return `a landing failed (${b.outcome})`
    case 'landing_declined': {
      const at = b.since ?? sameLanding(ctx, b.task_id, b.op)?.observed_at
      return `its landing was declined${at ? ` at ${clockWords(at, ctx.now)}` : ''}${reworked(b.rework) ? ' and reworked tasks landed in its place' : ' and no task replaces it'}`
    }
    case 'landing_unmapped': return `a landing is at ${b.phase}, which this page does not name`
    case 'stg_needs_landing': return 'STG waits for the landing'
    case 'stg_not_supported': return `${b.repo} has no STG release`
    case 'stg_withheld': return `it is withheld from STG (${b.kind})`
    case 'stg_batch_open': return `its STG batch ${b.batch_id} is ${b.batch_state}${b.quarantine ? ', quarantined' : ''}`
    case 'stg_not_admitted': {
      const k = [...b.blocks, ...b.repo_blocks][0]?.kind
      return `STG has not admitted it${k ? ` (${k})` : ''}`
    }
    case 'stg_exhausted': return 'every STG try is used'
    case 'stg_disposed': return 'it was disposed for STG'
  }
}

const VERDICT: Record<Sig, string> = { moving: 'Moving.', nobody: 'Nobody is on it.', unconfirmed: 'Unconfirmed.', waits: 'Waits.' }
type Base = Pick<StepWhyModel, 'readAt' | 'failed' | 'confirmed' | 'version' | 'stepVersion'>

/** An explained result whose rule is not true: the conclusion over its blockers, and its list. */
function explained(ctx: Ctx, s: Extract<StepExplain, { kind: 'explained' }>, base: Base): StepWhyModel {
  // One list for the whole step: each top-level blocker in turn, sharing what is listed (Counting above).
  const listed: Listed = { ids: new Set(), byBlock: new Map(), repoBlocks: null, memberUnlisted: 0, blocks: 0 }
  collectBlocks(s.blockers, listed)
  const parts = s.blockers.map(b => entriesOf({ ...ctx, listed }, [b]))
  const entries = parts.flatMap(p => p.entries)
  const rows = flatRows(entries)
  const blocksLeft = listed.repoBlocks !== null ? Math.max(0, listed.repoBlocks - listed.blocks) : listed.memberUnlisted
  const lastBlock = [...rows].reverse().find(r => r.kind === 'Block')
  if (blocksLeft > 0 && lastBlock) lastBlock.sub += ` · ${blocksLeft} more open ${blocksLeft === 1 ? 'block' : 'blocks'} not listed by the server`
  const omitted = (s.omitted ?? 0) + parts.reduce((n, p) => n + p.unlisted, 0) + blocksLeft
  const tasks = [...new Set(rows.flatMap(r => r.task ? [r.task] : []))]
  if (s.blockers.length === 0) {
    // Every blocker left out (over the 50 items), or none named: said so, never as nothing in the way.
    return omitted
      ? { ...base, word: 'Unconfirmed.', reason: `${omitted} ${omitted === 1 ? 'thing stands' : 'things stand'} in the way, none listed by the server (a step lists at most 50 items).`, muted: false, list: null, tasks }
      : { ...base, word: 'Unknown.', reason: `Its done rule reads ${s.rule}, but the read lists nothing that holds it.`, muted: false, list: null, tasks }
  }
  const sig = allSig(s.blockers.map(b => blockerSig(ctx, b)))
  const word = sig === 'waits' && s.rule === 'unknown' ? 'Unknown.' : VERDICT[sig]
  // "N more": everything in the way (rows + not listed) less what the two blockers it names took in the list (a shared
  // record is the first's); the blocks left out of the whole step are among the more.
  const named = s.blockers.slice(0, 2)
  const more = rows.length + omitted - parts.slice(0, 2).reduce((n, p) => n + flatRows(p.entries).length + p.unlisted, 0)
  const reason = `${cap([...new Set(named.map(b => phrase(ctx, b)))].join('; '))}${more > 0 ? `; ${more} more in the way` : ''}.`
  return { ...base, word, reason, muted: false, list: { entries, rows: rows.length, omitted }, tasks }
}

/** The tasks' word in the step's done rule (step_tasks state): accepted when it names none, or more than one. */
function tasksWord(rule: unknown): string {
  const states = new Set<string>()
  const walk = (r: unknown, depth: number) => {
    if (depth > 8 || r === null || typeof r !== 'object' || Array.isArray(r)) return
    const o = r as Record<string, unknown>
    const st = o.step_tasks as Record<string, unknown> | undefined
    if (st && typeof st === 'object') states.add(typeof st.state === 'string' ? st.state : 'accepted')
    for (const k of ['all', 'any']) if (Array.isArray(o[k])) (o[k] as unknown[]).forEach(c => walk(c, depth + 1))
  }
  walk(rule, 0)
  return states.size === 1 ? [...states][0] : 'accepted'
}

/** A conclusion about the read or the result as a whole, with no list. */
const said = (word: string, reason: string, step: PlanNode, more: Partial<StepWhyModel> = {}): StepWhyModel =>
  ({ word, reason, muted: true, readAt: null, failed: false, confirmed: true, version: null, stepVersion: step.version, list: null, tasks: [], ...more })

/**
 * Why step is not done, or null when this page says nothing of it: no explain block from this server, the plan view
 * itself not read now (old: viewOldReason's answer; the step's state already says Unknown and why), or a step not in
 * progress. The read is said first when it cannot be used: unreadable, pending, failed; then a failure computing this
 * step; then no result for it. A read older than EXPLAIN_OLD_MS, or of another version of the step than the plan view
 * holds (older or newer), concludes nothing about now: Unconfirmed, with when and of which version the evidence is; its
 * list stays as that read's. None of these is ever said as nothing in the way.
 */
export function stepWhy(view: PlanSnapshotRead, step: PlanNode, now: number, old: string): StepWhyModel | null {
  if (step.state !== 'active' || old || !view.explain) return null
  if (!view.explain.ok) return said('Reasons unknown.', `The explanation could not be read (${view.explain.error}). This does not mean nothing is in the way.`, step)
  const ex = view.explain.explain
  if (ex.status === 'pending') return said('Reasons pending.', 'The read has not answered yet.', step)
  if (ex.status === 'failed' || !ex.read_at) {
    const at = ex.last_attempt_at ? ` at ${clockWords(ex.last_attempt_at, now)}` : ''
    return said('Reasons unknown.', `The read failed (${ex.reason ?? 'error'}${at}). This does not mean nothing is in the way.`, step, { failed: true })
  }
  const readAt = ex.read_at, read = clockWords(readAt, now)
  const s = ex.steps[step.plan_node_id]
  if (!s) return said('No result.', `The ${read} read has no result for this step.`, step, { readAt })
  // A failure computing this step is said before the read's age or version (review round 11 M6).
  if (s.kind === 'incomplete') {
    const why = s.reason === 'timeout' ? 'it took too long' : `too much to read${s.which ? ` (${s.which})` : ''}`
    return said('Reasons incomplete.', `The read stopped for this step: ${why}. What it waits on is Unknown.`, step, { readAt })
  }
  if (s.kind === 'explain_error') return said('Reasons unknown.', `The read for this step failed (${s.code}). This does not mean nothing is in the way.`, step, { readAt })
  const stale = now - Date.parse(readAt) > EXPLAIN_OLD_MS
  const version = s.node_version
  const base: Base = { readAt, failed: false, confirmed: !stale && version === step.version, version, stepVersion: step.version }
  const drift = Math.max(0, (now - Date.parse(view.as_of)) / 1000)
  const ctx: Ctx = { view, now, drift, tasksWord: tasksWord(step.done_rule_json), readAt: Date.parse(readAt), later: Date.parse(view.as_of) > Date.parse(readAt) }
  const result = ((): StepWhyModel => {
    switch (s.kind) {
      case 'no_done_rule': return said('No done rule.', 'Nothing can be checked until one is set on the step.', step, base)
      case 'not_started': return said('Not started.', `The ${read} read has this step as not started.`, step, base)
      case 'rule_not_supported': return said('Not explained here.', `Its rule uses ${s.atoms.join(', ')}, which this page does not explain.`, step, base)
      case 'not_explained': return said('Not reached.', 'The read explains 20 open steps at a time; this one was not among them.', step, base)
    }
    if (s.rule === 'true') return said('Done by its rule.', 'Its done rule held at this read; the controller has not closed the step yet.', step, { ...base, muted: false })
    return explained(ctx, s, base)
  })()
  if (base.confirmed) return result
  // Not now: the evidence's time and version, and nothing concluded of now from it.
  const age = stale ? `, ${sinceWords(Date.parse(readAt), now)} ago (older than ${EXPLAIN_OLD_MS / 1000} s)` : ''
  const ver = version < step.version ? `; the step changed since (now version ${step.version})`
    : version > step.version ? `; this plan view still holds version ${step.version}, older than that read` : ''
  const evidence = `Reasons are from the ${read} read of version ${version}${age}${ver}, so what is in the way now is not confirmed.`
  return { ...result, word: 'Unconfirmed.', muted: true,
    reason: result.list ? `${evidence} Listed below as that read had it.` : `${evidence} That read said: ${result.word} ${result.reason}` }
}

/** The read states the module's steps share, for the line above them (Design v3.15 Stages banner); null when the read is usable. */
export type ExplainBanner = { kind: 'pending' | 'failed' | 'old' | 'changed'; text: string }
export function explainBanner(view: PlanSnapshotRead, steps: PlanNode[], now: number, old: string): ExplainBanner | null {
  const active = steps.filter(s => s.state === 'active')
  if (active.length === 0 || old || !view.explain) return null
  if (!view.explain.ok) return { kind: 'failed', text: `Could not read what the steps wait on: ${view.explain.error}. Every reason reads Unknown until a read succeeds.` }
  const ex = view.explain.explain
  if (ex.status === 'pending') return { kind: 'pending', text: 'Reading what each step waits on… Step states are current; reasons appear when the read answers.' }
  if (ex.status === 'failed' || !ex.read_at) {
    const at = ex.last_attempt_at ? ` at ${clockWords(ex.last_attempt_at, now)}` : ''
    return { kind: 'failed', text: `Could not read what the steps wait on: the read failed (${ex.reason ?? 'error'}${at}). Every reason reads Unknown until a read succeeds.` }
  }
  const read = clockWords(ex.read_at, now)
  if (now - Date.parse(ex.read_at) > EXPLAIN_OLD_MS) {
    return { kind: 'old', text: `Reasons are from ${read}, ${sinceWords(Date.parse(ex.read_at), now)} ago. Older than ${EXPLAIN_OLD_MS / 1000} s, so what is in the way now is not confirmed.` }
  }
  const differ = active.filter(s => { const r = ex.steps[s.plan_node_id]; return r && r.node_version !== s.version })
  if (differ.length > 0) return { kind: 'changed', text: `${differ.length === 1 ? 'One step' : `${differ.length} steps`} read at ${read} ${differ.length === 1 ? 'is' : 'are'} of another version than this plan view holds; ${differ.length === 1 ? 'its card says' : 'their cards say'} which.` }
  return null
}
