import type { SignoffWait } from '@/api/agentRuntimePlan'
import type { PlanSnapshot } from '@/api/planSnapshot'
import type { AcceptanceSignoff } from '@/api/planSnapshotSchema'
import type { Tone } from '../owner/ownerModel'
import type { ModuleRow } from './planModuleModel'
import { roundLabel, type RoundsRead } from '@/domains/plan/readings/rounds'
import { answeredWords, planTreeOld, progressOld, viewOldReason } from './readAge'
import type { SignoffWaitsRead } from './useRuntimePlan'

/*
 * Work's one-sentence summary and its counts (R5:5.5-2, Design v3.6 WorkPage), computed from what plan-view and
 * GET /plan answered, never written by hand. Every count is of the rows a condition picks, not a total less the others;
 * what cannot be read says Unknown with the condition that made it so. Claims and sources: the R5:5.5-2
 * hand-in report, and for one round's count the R5:5.5-4 one.
 */

export * from '@/domains/plan/observations/progressNarrative'

/** One row of the sign-offs: a step from GET /plan, an acceptance sign-off of plan-view, or an Owner step plan-view lists. */
export type SignoffRow =
  | { kind: 'step'; node: string; label: string; title: string; wait: SignoffWait }
  | { kind: 'acceptance'; node: string; label: string; title: string; signoff: AcceptanceSignoff }
  | { kind: 'owner-step'; node: string; label: string; title: string; reason: string }

/** How one source of the sign-offs was read: answered, being read, or Unknown with why. */
export type SourceRead = 'ok' | 'reading' | { unknown: string }

/**
 * The sign-offs as listed, each group with its own reach (R5:5.5-2 review: one "later" number mixed two):
 * - ready: step sign-offs whose work has started, of the rounds whose GET /plan was read (every round on the Sign-offs
 *   tab, the shown round's alone elsewhere: scope), and the shown round's acceptance sign-offs its plan view marks Ready;
 * - notStarted: step sign-offs whose work has not started, of the same rounds (GET /plan, TD-425);
 * - later: the shown round's plan-view owner_later only (acceptance sign-offs Blocked or Not started, Owner steps whose
 *   readiness it cannot judge): other rounds' are not read here;
 * with the rounds whose acceptance sign-offs are not counted, and how
 * each source was read (steps: GET /plan; round: the shown round's plan view). A source not read leaves its rows out,
 * and the counts say so; what the other source lists is still shown and signable.
 */
export type SignoffCount = {
  ready: SignoffRow[]; notStarted: SignoffRow[]; later: SignoffRow[]; uncounted: string[]; root: string | null
  /** The rounds whose GET /plan was read. */
  stepRounds: string[]
  steps: SourceRead; round: SourceRead
  /**
   * Whose step sign-offs were read: every round's GET /plan (the Sign-offs tab), or the shown round's alone (Work's
   * other tabs, R5:5.5-4: the summary no longer reads the earlier rounds' trees every 15 s), where nothing is said of the
   * other rounds but that the Sign-offs tab reads them.
   */
  scope: SignoffScope
  /**
   * What a source too old to be said as now, or whose refresh failed, last listed (R5:5.5-4): shown as last read, with
   * signing off, never in any count above. Empty while both sources are now.
   */
  lastRead: { ready: SignoffRow[]; notStarted: SignoffRow[]; later: SignoffRow[] }
}
export type SignoffScope = 'every' | 'round'
/** How many rows are listed as last read. */
export const lastReadRows = (c: SignoffCount) => c.lastRead.ready.length + c.lastRead.notStarted.length + c.lastRead.later.length

/**
 * What plan-view the sign-offs are counted with: its last answer (if any), why it failed, the round, the rounds read,
 * and now: with it, a read too old to be said as now is Unknown (readAge.ts; R5:5.5-4), without it no age is judged.
 */
export type ViewRead = { view: PlanSnapshot | undefined; failed: string | null; root: string | null; rounds: RoundsRead; scope?: SignoffScope; now?: number }

/** Why the round list (GET /progress) is not now ('' when it is): its latest read failed, or it is old. */
function roundsOldReason(rounds: RoundsRead, now: number | undefined): string {
  if (rounds.state !== 'ok') return ''
  if (rounds.failed) return `GET /progress failed (${rounds.failed}); the rounds are as last read`
  return now !== undefined && progressOld(rounds.at, now) ? `GET /progress ${answeredWords(rounds.at, now)}` : ''
}

/** The plan view held, when it is now: one that failed since, or is old, is not used (R5:5.5-4). */
function viewOld(read: ViewRead): string {
  if (!read.view) return ''
  // The age needs a clock; a refresh that failed is known without one.
  return read.now !== undefined ? viewOldReason(read.view, read.view.root, read.failed, read.now)
    : read.failed ? viewOldReason(read.view, read.view.root, read.failed, Date.parse(read.view.as_of)) : ''
}

function roundRead(read: ViewRead): SourceRead {
  if (read.view !== undefined) {
    const old = viewOld(read)
    return old ? { unknown: old } : 'ok'
  }
  if (read.root === null) {
    // No round to read yet: still reading which is newest, Unknown, or (only when the list was read, and is now) none held.
    if (read.rounds.state === 'reading') return 'reading'
    if (read.rounds.state === 'unknown') return { unknown: `which round to read is Unknown (${read.rounds.reason})` }
    const old = roundsOldReason(read.rounds, read.now)
    if (read.rounds.newest === null) return { unknown: old ? `which round to read is Unknown (${old})` : 'the runtime holds no round' }
    return 'reading'
  }
  if (read.failed) return { unknown: `${roundLabel(read.root)}'s plan view could not be read (${read.failed})` }
  return 'reading'
}

/** Why the GET /plan answers are not now ('' when they are): the oldest older than twice their 15 s poll. */
function treesOldReason(waits: SignoffWaitsRead, now: number | undefined): string {
  if (waits.read !== 'ok' || now === undefined || waits.at === undefined || !planTreeOld(waits.at, now)) return ''
  return `GET /plan?root=${waits.oldest ?? '?'} ${answeredWords(waits.at, now)}, older than twice its 15 s poll`
}

/**
 * GET /plan for every round is whole only when the round list it was read for is (R5:5.5-2 review): rounds not read
 * (GET /progress failed or paused, answered with no plan list, or listed no round with an R<n> id) leave PLAN_ROOT read
 * alone; ids not R<n> beside good ones are never read. Either way the step sign-offs are Unknown, naming what was read.
 */
function stepsRead(waits: SignoffWaitsRead, rounds: RoundsRead, now?: number): SourceRead {
  if (waits.read === 'error') return { unknown: 'the plan could not be read (GET /plan)' }
  if (waits.read !== 'ok') return 'reading'
  const old = treesOldReason(waits, now)
  if (old) return { unknown: old }
  const read = (waits.rounds ?? []).length ? `only ${(waits.rounds ?? []).join(', ')} read` : 'no round read'
  if (rounds.state === 'reading') return 'reading'
  if (rounds.state === 'unknown') return { unknown: `${read}; which rounds exist is Unknown (${rounds.reason})` }
  const listOld = roundsOldReason(rounds, now)
  if (listOld) return { unknown: `${read}; which rounds exist is Unknown (${listOld})` }
  if (rounds.ignored.length) return { unknown: `round${rounds.ignored.length === 1 ? '' : 's'} ${rounds.ignored.join(', ')} not read: ${rounds.ignored.length === 1 ? 'its id is' : 'their ids are'} not R<n>` }
  return 'ok'
}

/**
 * The shown round's GET /plan alone (R5:5.5-4): whole once it answered, whatever the round list holds; while the round
 * is not known, said as the plan view's read says it (which round to read is being read, Unknown, or none held).
 */
function roundStepsRead(waits: SignoffWaitsRead, read: ViewRead): SourceRead {
  if (read.root === null) return roundRead({ ...read, view: undefined })
  if (waits.read === 'error') return { unknown: `${roundLabel(read.root)}'s plan could not be read (GET /plan?root=${read.root})` }
  if (waits.read !== 'ok') return 'reading'
  const old = treesOldReason(waits, read.now)
  return old ? { unknown: old } : 'ok'
}

/**
 * The sign-offs, from the two reads Work already makes: GET /plan (every round's on the Sign-offs tab, the shown
 * round's alone elsewhere: scope; a step whose done rule waits on an unsigned owner_signoff: ready once its work has
 * started, not started while it has not; TD-425) and the shown round's plan-view (owner_waiting: acceptance sign-offs
 * Ready; owner_later: later). A sign-off both list is one row; an Owner step plan-view
 * lists without a sign-off is a later row unless GET /plan lists it. A read too old to be said as now (readAge.ts), or
 * whose refresh failed, is Unknown with why, and its rows are left out of every count: never "0 ready" from it. They
 * are kept in lastRead, to be listed as last read with signing off (R5:5.5-4).
 */
export function signoffCount(waits: SignoffWaitsRead, read: ViewRead): SignoffCount {
  const scope = read.scope ?? 'every'
  const steps = scope === 'round' ? roundStepsRead(waits, read) : stepsRead(waits, read.rounds, read.now)
  const round = roundRead(read)
  // GET /plan's rows: counted unless its answers are old; a failed refresh's last answers are last read.
  const treesNow = waits.read === 'ok' && !treesOldReason(waits, read.now)
  const trees = waits.read === 'ok' ? waits : waits.read === 'error' ? waits.last : undefined
  // The plan view's rows: counted while it is now, else last read.
  const view = read.view
  const viewNow = !!view && !viewOld(read)
  const counted = { ready: [] as SignoffRow[], notStarted: [] as SignoffRow[], later: [] as SignoffRow[] }
  const last = { ready: [] as SignoffRow[], notStarted: [] as SignoffRow[], later: [] as SignoffRow[] }
  const node = (id: string) => view?.nodes.find(n => n.plan_node_id === id)
  // node|ref of the step rows counted (signed) and listed as last read (lastSigned): a sign-off both sources list is one
  // row. An acceptance row is not added beside a counted step row, nor, as last read, beside a last-read one; a last-read
  // step row is dropped beside an acceptance row counted (ready or later) from a view that is now (review of 13191da1:
  // with both sources old, or GET /plan old and the view now listing it later, the same sign-off was listed twice).
  const signed = new Set<string>()
  const lastSigned = new Set<string>()
  const keys = (w: SignoffWait) => w.refs.map(ref => `${w.node}|${ref}`)
  const stepRow = (w: SignoffWait, now: boolean): SignoffRow => {
    keys(w).forEach(k => (now ? signed : lastSigned).add(k))
    return { kind: 'step', node: w.node, label: w.label, title: w.title, wait: w }
  }
  for (const w of trees?.waits ?? []) (treesNow ? counted : last).ready.push(stepRow(w, treesNow))
  for (const w of trees?.notStarted ?? []) (treesNow ? counted : last).notStarted.push(stepRow(w, treesNow))
  const into = viewNow ? counted : last
  const acceptance = (s: AcceptanceSignoff): SignoffRow[] => {
    const key = `${s.plan_node_id}|${s.ref}`
    if (signed.has(key) || (!viewNow && lastSigned.has(key))) return []
    return [{ kind: 'acceptance', node: s.plan_node_id, label: node(s.plan_node_id)?.label ?? s.plan_node_id, title: node(s.plan_node_id)?.title ?? '', signoff: s }]
  }
  for (const w of view?.owner_waiting ?? []) into.ready.push(...acceptance(w))
  const all = () => [...counted.ready, ...counted.notStarted, ...counted.later, ...last.ready, ...last.notStarted, ...last.later]
  for (const w of view?.owner_later ?? []) {
    if ('ref' in w) { into.later.push(...acceptance(w)); continue }
    if (all().some(r => r.node === w.plan_node_id)) continue
    into.later.push({ kind: 'owner-step', node: w.plan_node_id, label: node(w.plan_node_id)?.label ?? w.plan_node_id, title: w.title, reason: w.reason })
  }
  const countedAcceptance = new Set([...counted.ready, ...counted.later].flatMap(r => r.kind === 'acceptance' ? [`${r.node}|${r.signoff.ref}`] : []))
  const beside = (r: SignoffRow) => r.kind === 'step' && keys(r.wait).some(k => countedAcceptance.has(k))
  last.ready = last.ready.filter(r => !beside(r))
  last.notStarted = last.notStarted.filter(r => !beside(r))
  const root = read.view?.root ?? read.root
  return { ...counted, uncounted: waits.read === 'ok' ? waits.acceptanceRoots.filter(r => r !== root) : [], root,
    stepRounds: waits.read === 'ok' ? waits.rounds ?? [] : [], round, scope, steps, lastRead: last }
}

/** Both sources answered: the counts are whole for what they cover. */
export const signoffsComplete = (c: SignoffCount) => c.steps === 'ok' && c.round === 'ok'
/** Why the counts are not whole: each source not read, with its reason. */
export const signoffGaps = (c: SignoffCount) => [c.steps, c.round].flatMap(s => typeof s === 'object' ? [s.unknown] : [])
const roundName = (c: SignoffCount) => c.root ? roundLabel(c.root) : 'the round'
/**
 * Where the step sign-offs were read: every round when the list was whole, else the rounds read; scoped to one round,
 * that round only (R5:5.5-4), whether it answered or not.
 */
export const stepsReach = (c: SignoffCount) => c.scope === 'round' ? `${roundName(c)} only`
  : c.steps === 'ok' ? 'every round' : c.stepRounds.length ? `${c.stepRounds.join(', ')} only` : 'no round read'
/** Where the shown round's step sign-offs come from: GET /plan?root=R5 one round; GET /plan, every round. */
const stepsSource = (c: SignoffCount) => c.scope === 'round' && c.root ? `GET /plan?root=${c.root}` : 'GET /plan'
/** Said beside a count of one round's sign-offs (R5:5.5-4): the other rounds are read on the Sign-offs tab, never 0 here. */
export const OTHER_ROUNDS = 'other rounds on the Sign-offs tab'

/**
 * The tab's and the summary's words for the count, each part from its own source (R5:5.5-2 review): "reading…" while
 * a source is being read and none failed; "Unknown" when neither source could be read (or one failed while the other is
 * read); else "0 ready · [n not started ·] 13 later in Round 5", where a source not read says so in its own part:
 * "later in Round 5 Unknown" for the plan view, "not started Unknown" (or "n+ not started" when the rounds read list
 * some) for GET /plan, and ready, which takes both, is "n+ ready". Scoped to one round (R5:5.5-4), every part is that
 * round's and the words end so: "0 ready · 13 later · Round 5 only".
 */
export function signoffWords(count: SignoffCount): string {
  const gaps = signoffGaps(count)
  const reading = count.steps === 'reading' || count.round === 'reading'
  if (gaps.length === 0 && reading) return 'reading…'
  if (gaps.length === 2 || (gaps.length > 0 && reading)) return 'Unknown'
  const ready = `${count.ready.length}${gaps.length ? '+' : ''} ready`
  const notStarted = count.steps === 'ok' ? (count.notStarted.length ? [`${count.notStarted.length} not started`] : [])
    : [count.notStarted.length ? `${count.notStarted.length}+ not started` : 'not started Unknown']
  if (count.scope === 'round') return [ready, ...notStarted, count.round === 'ok' ? `${count.later.length} later` : 'later Unknown', `${roundName(count)} only`].join(' · ')
  const later = count.round === 'ok' ? `${count.later.length} later in ${roundName(count)}` : `later in ${roundName(count)} Unknown`
  return [ready, ...notStarted, later].join(' · ')
}

const many = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`

/** Each count with its source and reach, for a title and for "How this is read". */
export function signoffBreakdown(count: SignoffCount): string[] {
  const round = roundName(count)
  const acceptReady = count.ready.filter(r => r.kind === 'acceptance').length
  return [
    `Ready ${count.ready.length}: ${many(count.ready.length - acceptReady, 'step sign-off')} whose work has started, ${stepsReach(count)} (${stepsSource(count)}); ${many(acceptReady, 'acceptance sign-off')} ${round}'s plan view marks Ready.`,
    `Not started ${count.notStarted.length}: step sign-offs whose work has not started, ${stepsReach(count)} (${stepsSource(count)}).`,
    `Later in ${round} ${count.later.length}: ${round}'s plan view only (owner_later: acceptance sign-offs Blocked or Not started, Owner steps whose readiness is Unknown); other rounds' are not read here.`,
    ...(count.scope === 'round' ? ['Other rounds are not read here: the Sign-offs tab reads every round\'s step sign-offs and lists the acceptance sign-offs of the other rounds.'] : []),
    ...(count.uncounted.length ? [`Acceptance sign-offs of ${count.uncounted.join(', ')} are not counted here (listed on Sign-offs).`] : []),
    ...signoffGaps(count).map(g => `Unknown: ${g}.`),
  ]
}

/**
 * The summary's lamp and why: attention (yellow) when a sign-off is ready or a step is
 * stopped; otherwise unknown (grey) while anything it would need is not known: the plan view old or unread, the
 * mainline Unknown, a source of the sign-offs not read, another round's acceptance sign-offs not counted, or an Owner
 * step's readiness Unknown; ok (green) only when none holds, and then only for what was read: with one round's step
 * sign-offs read (R5:5.5-4), that round only, never "any round".
 */
export function summaryLamp({ stale, mainlineUnknown, count, rows }: { stale: boolean; mainlineUnknown: boolean; count: SignoffCount; rows: ModuleRow[] }): { tone: Tone; why: string[] } {
  const round = roundName(count)
  const stopped = rows.filter(r => r.stopped > 0).length
  const attention = [
    ...(count.ready.length ? [`${count.ready.length} sign-off${count.ready.length === 1 ? '' : 's'} ready for you${count.scope === 'round' ? ` in ${round}` : ''}`] : []),
    ...(stopped ? [`${stopped} module${stopped === 1 ? '' : 's'} in ${round} with a stopped step`] : []),
  ]
  if (attention.length) return { tone: 'attention', why: attention }
  const owner = count.later.filter(r => r.kind === 'owner-step').length
  const unknown = [
    ...(stale ? [`${round}'s plan view is old or could not be read`] : []),
    ...(mainlineUnknown ? ['the mainline is Unknown'] : []),
    ...signoffGaps(count).map(g => `sign-offs: ${g}`),
    ...(count.steps === 'reading' || count.round === 'reading' ? ['the sign-offs are still being read'] : []),
    ...(count.uncounted.length ? [`acceptance sign-offs of ${count.uncounted.join(', ')} are not counted here`] : []),
    ...(owner ? [`${owner} Owner step${owner === 1 ? '\'s' : 's\''} readiness in ${round} is Unknown`] : []),
  ]
  if (unknown.length) return { tone: 'unknown', why: unknown }
  if (count.scope === 'round') return { tone: 'ok', why: [`in ${round} only, no step sign-off ready, no acceptance sign-off ready, no step stopped; other rounds are read on the Sign-offs tab`] }
  return { tone: 'ok', why: [`no step sign-off ready in any round; in ${round}, no acceptance sign-off ready, no step stopped`] }
}
