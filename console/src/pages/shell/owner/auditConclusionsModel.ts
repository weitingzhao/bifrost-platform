import type { Block, Cell } from '@bifrost/ui/city/cards'
import type { RegisterLaw } from '@/api/lawRegister'
import { gradeTone, lawHref, planNodeHref } from './lawRegisterModel'

/*
 * Command → Audit: the audit's conclusions (箭10 2026-11-24; the Owner: 「审计基于立法的标准出结果，来监督行政」).
 * Every standard of every law, one row each:
 * - its result: reached / missed / unmeasured, from the step's grade;
 * - for a missed one, its cause: law, agent or tool;
 * - the plan step it is judged on (R6:6.x-n), linked;
 * - opened, its evidence: the standard, how it is measured, where it stands now, and the card that set it.
 * The data is the law register as the UI engine serves it (/engine/v1/agents?read=laws).
 * The cause is the audit line's call (get_approval_readings), not served yet. Until it is, a missed standard's cause
 * says unknown and why; nothing is guessed from the text.
 */

export type Result = 'reached' | 'missed' | 'unmeasured'
const TONE: Record<Result, 'ok' | 'warn' | 'unknown'> = { reached: 'ok', missed: 'warn', unmeasured: 'unknown' }

/** The causes the audit names (law, agent, tool); null until the audit's readings are served. */
export type Cause = 'law' | 'agent' | 'tool'

export type StandardRow = {
  law: string
  lawText: string
  planItem: string
  step: string
  /** s1, s2…: the standard's own number, when its title has one. */
  label: string
  standard: string
  method: string
  now: string
  grade: string
  result: Result
  cause: Cause | null
  card?: string
  /** From the step's own events (plan_step_judged → its note): the reading as the audit wrote it, and its source. */
  reading?: Reading
  attribution?: Attribution | null
}

/** A plan step's events as the platform serves them (GET /api/v1/agentrt/plan/{node} events). */
export type PlanEvent = { event_id: number; event_type: string; body?: Record<string, unknown> }

/** The audit's reading of a step: the note its latest verdict points at, and whether its numbers are kept by the system or read once. */
export type Reading = { text: string; source: 'archived' | 'ad hoc' | 'mixed' | 'unsaid'; noteEventId: number; found: boolean }

/** The audit's cause of a missed standard (a note "归因 sN：… 类别 X … 原因：… ｜去向：…"), the latest one on the step. */
export type Attribution = { category: Cause | 'unchecked'; reason: string; to: string; noteEventId: number }

const textOf = (e: PlanEvent | undefined) => (e?.body && typeof e.body.text === 'string' ? e.body.text : '')

/**
 * The step's reading (Audit 2026-11-24, ui-audit-page-fields): the plan_step_judged with the largest event_id, then the
 * note it names (note_event_id). The text says 系统存档 (kept by the system) or 临时 (read once), often both.
 */
export function readingOf(events: readonly PlanEvent[]): Reading | undefined {
  const judged = events.filter(e => e.event_type === 'plan_step_judged').sort((a, b) => b.event_id - a.event_id)[0]
  const id = judged?.body?.note_event_id
  if (typeof id !== 'number') return undefined
  const text = textOf(events.find(e => e.event_id === id))
  // The engine passes the step's last 50 events: an older note is not among them, said so, never guessed.
  if (!text) return { text: '', source: 'unsaid', noteEventId: id, found: false }
  const archived = text.includes('系统存档'), adhoc = text.includes('临时')
  return { text, noteEventId: id, found: true, source: archived && adhoc ? 'mixed' : archived ? 'archived' : adhoc ? 'ad hoc' : 'unsaid' }
}

const CATEGORY: Record<string, Cause | 'unchecked'> = { '法': 'law', 'Agent': 'agent', '工具': 'tool', '没核': 'unchecked' }

/**
 * The step's cause, from the latest note that starts 「归因 sN：」 (no structured event yet; Audit has asked the
 * legislation line for plan_step_attributed). A later note that says 作废 withdraws it: then there is none.
 */
export function attributionOf(events: readonly PlanEvent[], label: string): Attribution | null {
  if (!label) return null
  const latest = events.filter(e => e.event_type === 'note' && textOf(e).startsWith(`归因 ${label}：`)).sort((a, b) => b.event_id - a.event_id)[0]
  if (!latest) return null
  const text = textOf(latest)
  if (text.includes('作废')) return null
  const category = CATEGORY[/类别\s*(法|Agent|工具|没核)/.exec(text)?.[1] ?? ''] ?? 'unchecked'
  const reason = /原因[：:]\s*([^｜]*)/.exec(text)?.[1]?.trim() ?? ''
  const to = /去向[：:]\s*([^｜]*)/.exec(text)?.[1]?.trim() ?? ''
  return { category, reason, to, noteEventId: latest.event_id }
}

/** A row with its step's events read in. */
export function withEvents(r: StandardRow, events: readonly PlanEvent[] | undefined): StandardRow {
  if (!events) return r
  const attribution = attributionOf(events, r.label)
  const cause = attribution && attribution.category !== 'unchecked' ? attribution.category : null
  return { ...r, reading: readingOf(events), attribution, cause }
}

/** A step's grade as a result: 达到 reached; 部分 and 没达到 missed (部分 is not reached); 没核 or none unmeasured. */
export function resultOf(grade: string | undefined): Result {
  const t = gradeTone(grade)
  return t === 'ok' ? 'reached' : t === 'warn' ? 'missed' : 'unmeasured'
}

/**
 * A standard's title as the plan line writes it: "s1：<standard>｜量法：<how it is measured>…现在：<where it stands>".
 * A title not in that shape is kept whole as the standard.
 */
export function splitStandard(title: string): { label: string; standard: string; method: string; now: string } {
  const [head, rest = ''] = title.split('｜量法：')
  const m = /^(s\d+)[：:]\s*/.exec(head)
  const label = m ? m[1] : ''
  const standard = m ? head.slice(m[0].length) : head
  const at = rest.lastIndexOf('现在：')
  return { label, standard: standard.trim(), method: (at >= 0 ? rest.slice(0, at) : rest).trim(), now: at >= 0 ? rest.slice(at + 3).trim() : '' }
}

/** Every standard of every law that has some, in law then step order. */
export function standardsOf(laws: readonly RegisterLaw[]): StandardRow[] {
  return laws.flatMap(l => (l.steps ?? []).map(s => {
    const parts = splitStandard(s.title)
    return {
      law: l.law, lawText: l.text, planItem: l.plan_item ?? '', step: s.plan_node_id,
      ...parts, grade: s.grade ?? '', result: resultOf(s.grade), cause: null,
      ...(s.version ? { card: s.version } : {}),
    }
  }))
}

/** The page's verdict: "10 standards in 2 laws · 2 reached · 6 missed · 2 unmeasured · 83 laws have no standards yet". */
export function auditVerdict(laws: readonly RegisterLaw[] | null): string {
  if (laws == null) return '? standards · ? reached · ? missed · ? unmeasured'
  const rows = standardsOf(laws)
  const judged = new Set(rows.map(r => r.law)).size
  const by = (r: Result) => rows.filter(x => x.result === r).length
  const parts = [`${rows.length} standards in ${judged} laws`, `${by('reached')} reached`, `${by('missed')} missed`, `${by('unmeasured')} unmeasured`]
  const none = laws.length - judged
  if (none > 0) parts.push(`${none} laws have no standards yet`)
  return parts.join(' · ')
}

/** The page's word: any standard missed is warn; all reached is ok; otherwise unknown (not read, or nothing measured). */
export function auditWord(laws: readonly RegisterLaw[] | null): 'ok' | 'warn' | 'unknown' {
  if (laws == null) return 'unknown'
  const rows = standardsOf(laws)
  if (rows.some(r => r.result === 'missed')) return 'warn'
  if (rows.length > 0 && rows.every(r => r.result === 'reached')) return 'ok'
  return 'unknown'
}

export const CAUSE_UNREAD = 'Cause not read: this step’s events could not be read'

export const CAUSE_NONE = 'The audit has not attributed this missed standard yet'
export const CAUSE_UNCHECKED = 'The audit attributed it as not checked (没核)'

function causeCell(r: StandardRow): Cell {
  if (r.result !== 'missed') return '—'
  if (r.cause) return { text: r.cause, title: r.attribution ? `${r.attribution.reason}${r.attribution.to ? ' → ' + r.attribution.to : ''}` : r.cause }
  return { text: 'unknown', tone: 'unknown', title: r.attribution === undefined ? CAUSE_UNREAD : r.attribution?.category === 'unchecked' ? CAUSE_UNCHECKED : CAUSE_NONE }
}

/** The conclusions as a Ledger table, one row per standard; the key is the plan step, so a row can be linked. */
export function auditTable(rows: readonly StandardRow[]): Extract<Block, { kind: 'table' }> {
  return {
    kind: 'table',
    head: [{ label: 'Law', num: true }, { label: 'Standard' }, { label: 'Result' }, { label: 'Cause' }, { label: 'Plan step' }],
    rows: rows.map(r => ({
      key: r.step,
      cells: [
        r.law,
        { text: (r.label ? r.label + ' · ' : '') + r.standard, title: r.standard },
        { text: r.result, tone: TONE[r.result], title: r.grade || 'no verdict yet' },
        causeCell(r),
        r.step,
      ],
    })),
  }
}

/** A row opened: its evidence as the plan line wrote it, and where to look further. */
export function auditDetail(r: StandardRow | undefined): Block[] | null {
  if (!r) return null
  const facts: [string, string][] = [
    ['Standard', r.standard],
    ['How it is measured', r.method || 'not written'],
    ['Where it stands', r.now || 'not written'],
    ['Verdict', r.grade || 'no verdict yet'],
    ['Reading', r.reading ? (r.reading.found ? r.reading.text : `note ${r.reading.noteEventId} is not among the step’s last 50 events`) : r.attribution === undefined ? 'not read: this step’s events could not be read' : 'no reading posted'],
    ['Numbers', r.reading?.found ? { archived: 'kept by the system', 'ad hoc': 'read once (ad hoc)', mixed: 'some read once (ad hoc)', unsaid: 'not said' }[r.reading.source] : '—'],
    ['Cause', r.result !== 'missed' ? '—' : r.attribution ? `${r.attribution.category} · ${r.attribution.reason}${r.attribution.to ? ' → ' + r.attribution.to : ''}` : r.attribution === undefined ? CAUSE_UNREAD : CAUSE_NONE],
    ['Law', `${r.law} · ${r.lawText}`],
    ['Set by card', r.card ?? 'not recorded'],
  ]
  return [
    { kind: 'table', head: [{ label: 'Evidence' }, { label: '' }], rows: facts.map(([k, v]) => ({ key: k, cells: [k, v] })) },
    { kind: 'table', head: [{ label: 'Open' }], rows: [
      { key: 'step', cells: [`Plan step ${r.step}`], href: planNodeHref(r.step) },
      ...(r.planItem ? [{ key: 'item', cells: [`Plan item ${r.planItem}`], href: planNodeHref(r.planItem) }] : []),
      { key: 'law', cells: [`Law ${r.law} in the register`], href: lawHref(r.law) },
    ] },
  ]
}
