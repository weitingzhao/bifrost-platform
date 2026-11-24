import type { Block, Cell } from '@bifrost/ui/city/cards'
import type { LawStep, RegisterLaw } from '@/api/lawRegister'
import { gradeTone, lawHref, planNodeHref } from './lawRegisterModel'

/*
 * Command → Audit conclusions (箭10 2026-11-24; the Owner: 「审计基于立法的标准出结果，来监督行政」; the audit line's
 * brief, domains/2-lineage/audit/ui-brief-audit-page-2026-11-24.md). One row per standard, grouped by law.
 * The conclusion and the cause are apart:
 * - the conclusion: reached / missed / unmeasured, and whether its numbers are kept by the system or read once;
 * - the cause: law / agent / tool, only for a missed one.
 * Evidence is one click away. Unmeasured is not missed: an unmeasured standard lists what read it still lacks.
 * The page's three numbers are GET /laws' own counts, never recounted here.
 * Everything is read through the UI engine:
 * - /engine/v1/agents?read=laws for the register;
 * - read=plan-step for a step's events;
 * - read=approval-readings for law 85 s1.
 */

/**
 * Where this page reads through the engine, by the platform's viewer_env (the Laws page's switch, 箭10 2026-11-24):
 * PROD has no engine yet, so it reads the platform directly there. Either way a failed read says why; never a quiet
 * fall back from one way to the other.
 */
export const ENGINE_AUDIT: Readonly<Partial<Record<string, boolean>>> = { stg: true }
export const engineAuditOn = (env: string | null | undefined): boolean => {
  const key = (env ?? '').trim().toLowerCase()
  return key !== '' && Object.hasOwn(ENGINE_AUDIT, key) && ENGINE_AUDIT[key] === true
}

export type Result = 'reached' | 'missed' | 'unmeasured'
const TONE: Record<Result, 'ok' | 'warn' | 'unknown'> = { reached: 'ok', missed: 'warn', unmeasured: 'unknown' }
/** Missed first, unmeasured last (the audit line's default). */
const ORDER: Record<Result, number> = { missed: 0, reached: 1, unmeasured: 2 }

export type Cause = 'law' | 'agent' | 'tool'
const CATEGORY: Record<string, Cause | 'unchecked'> = { '法': 'law', 'Agent': 'agent', '工具': 'tool', '没核': 'unchecked' }

/** A plan step's events as the platform serves them (GET /api/v1/agentrt/plan/{node} events, the last 50). */
export type PlanEvent = { event_id: number; event_type: string; body?: Record<string, unknown> }

/** The audit's reading of a step: the note its latest verdict points at; its numbers kept by the system or read once. */
export type Reading = { text: string; source: 'archived' | 'ad hoc' | 'mixed' | 'unsaid'; noteEventId: number; judgedEventId: number; found: boolean }

/**
 * The audit's cause of a step: structured (GET /laws steps[].attribution, plan_step_attributed) when there, else the
 * latest 「归因 sN：」 note. A note whose category cannot be read is kept as raw text, its category unknown.
 */
export type Attribution = { category: Cause | 'unchecked' | 'unreadable'; reason: string; to: string; version?: string; from: 'event' | 'note'; raw?: string; noteEventId?: number }

export type StandardRow = {
  law: string
  lawText: string
  planItem: string
  step: string
  label: string
  standard: string
  method: string
  now: string
  grade: string
  result: Result
  card?: string
  attribution: Attribution | null | undefined
  /** undefined: the step's events are not read (yet); else what they say. */
  reading?: Reading | null
}

const textOf = (e: PlanEvent | undefined) => (e?.body && typeof e.body.text === 'string' ? e.body.text : '')

/** A step's grade as a result: 达到 reached; 部分 and 没达到 missed (not reached); 没核 or none unmeasured. */
export function resultOf(grade: string | undefined): Result {
  const t = gradeTone(grade)
  return t === 'ok' ? 'reached' : t === 'warn' ? 'missed' : 'unmeasured'
}

/** "s1：<standard>｜量法：<how it is measured>…现在：<where it stands>"; another shape is kept whole as the standard. */
export function splitStandard(title: string): { label: string; standard: string; method: string; now: string } {
  const [head, rest = ''] = title.split('｜量法：')
  const m = /^(s\d+)[：:]\s*/.exec(head)
  const at = rest.lastIndexOf('现在：')
  return {
    label: m ? m[1] : '',
    standard: (m ? head.slice(m[0].length) : head).trim(),
    method: (at >= 0 ? rest.slice(0, at) : rest).trim(),
    now: at >= 0 ? rest.slice(at + 3).trim() : '',
  }
}

/** The step's reading: the latest plan_step_judged and the note it names (an older note than the last 50 is said so). */
export function readingOf(events: readonly PlanEvent[]): Reading | null {
  const judged = events.filter(e => e.event_type === 'plan_step_judged').sort((a, b) => b.event_id - a.event_id)[0]
  const id = judged?.body?.note_event_id
  if (!judged || typeof id !== 'number') return null
  const text = textOf(events.find(e => e.event_id === id))
  if (!text) return { text: '', source: 'unsaid', noteEventId: id, judgedEventId: judged.event_id, found: false }
  const archived = text.includes('系统存档'), adhoc = text.includes('临时')
  return { text, noteEventId: id, judgedEventId: judged.event_id, found: true, source: archived && adhoc ? 'mixed' : archived ? 'archived' : adhoc ? 'ad hoc' : 'unsaid' }
}

// 「归因 s4：」, or with a correction's parenthesis 「归因 s4（更正）：」 (the platform's own pattern, planstore 0046).
const ATTRIBUTION = (label: string) => new RegExp(`^\\s*归因\\s*${label}\\s*(?:（[^）]*）|\\([^)]*\\))?\\s*[:：]`)

/** From the latest attribution note on the step (no structured event yet); a later 作废 withdraws it. */
export function attributionOf(events: readonly PlanEvent[], label: string): Attribution | null {
  if (!label) return null
  const re = ATTRIBUTION(label)
  const latest = events.filter(e => e.event_type === 'note' && re.test(textOf(e))).sort((a, b) => b.event_id - a.event_id)[0]
  if (!latest) return null
  const text = textOf(latest)
  if (text.includes('作废')) return null
  const c = /类别\s*(法|Agent|工具|没核)/.exec(text)?.[1]
  const reason = /原因[：:\s]\s*([^｜]*)/.exec(text)?.[1]?.trim() ?? ''
  const to = /去向[：:\s]\s*([^｜]*)/.exec(text)?.[1]?.trim() ?? ''
  const version = /appr_[0-9a-f]+/.exec(text)?.[0]
  return { category: c ? CATEGORY[c] : 'unreadable', reason, to, ...(version ? { version } : {}), from: 'note', noteEventId: latest.event_id, ...(c ? {} : { raw: text }) }
}

/** The structured attribution GET /laws carries on a step, when there. */
function attributionFromStep(s: LawStep): Attribution | null {
  const a = s.attribution
  if (!a) return null
  return { category: CATEGORY[a.category] ?? 'unreadable', reason: a.cause ?? '', to: a.destination ?? '', ...(a.version ? { version: a.version } : {}), from: 'event', ...(CATEGORY[a.category] ? {} : { raw: a.category }) }
}

/** The current steps of every law that has some, the law's own plan item only (an older item, such as R6:6.10, is folded away). */
export function standardsOf(laws: readonly RegisterLaw[]): StandardRow[] {
  return laws.flatMap(l => (l.steps ?? [])
    .filter(s => !l.plan_item || s.plan_node_id.startsWith(l.plan_item + '-'))
    .map(s => {
      const structured = attributionFromStep(s)
      return {
        law: l.law, lawText: l.text, planItem: l.plan_item ?? '', step: s.plan_node_id,
        ...splitStandard(s.title), grade: s.grade ?? '', result: resultOf(s.grade),
        // A structured attribution is read with the register; otherwise the step's notes are read (undefined until they are).
        attribution: structured ?? undefined,
        ...(s.version ? { card: s.version } : {}),
      }
    }))
}

/** A row with its step's events read in (the structured attribution, when there, stays). */
export function withEvents(r: StandardRow, events: readonly PlanEvent[] | undefined): StandardRow {
  if (!events) return r
  return { ...r, reading: readingOf(events), attribution: r.attribution?.from === 'event' ? r.attribution : attributionOf(events, r.label) }
}

/** Grouped by law, then missed first and unmeasured last. */
export function ordered(rows: readonly StandardRow[]): StandardRow[] {
  return [...rows].sort((a, b) => Number(a.law) - Number(b.law) || a.law.localeCompare(b.law) || ORDER[a.result] - ORDER[b.result] || a.step.localeCompare(b.step, undefined, { numeric: true }))
}

/** The three numbers as GET /laws gives them (summed over laws, never recounted from the steps). */
export function lawCounts(laws: readonly RegisterLaw[]): { reached: number; missed: number; unmeasured: number; laws: number } {
  const counted = laws.filter(l => l.counts != null)
  const sum = (k: 'reached' | 'missed' | 'unmeasured') => counted.reduce((n, l) => n + (l.counts?.[k] ?? 0), 0)
  return { reached: sum('reached'), missed: sum('missed'), unmeasured: sum('unmeasured'), laws: counted.length }
}

/** A missed standard with no cause yet (none, 没核, or unreadable); null while its events are not read. */
const unattributed = (r: StandardRow): boolean | null => r.result !== 'missed' ? false : r.attribution === undefined ? null : !r.attribution || r.attribution.category === 'unchecked' || r.attribution.category === 'unreadable'

/** "2 reached · 6 missed · 3 unmeasured (2 laws, from GET /laws) · 5 missed without a cause". */
export function auditVerdict(laws: readonly RegisterLaw[] | null, rows: readonly StandardRow[]): string {
  if (laws == null) return '? reached · ? missed · ? unmeasured'
  const c = lawCounts(laws)
  const parts = [`${c.reached} reached`, `${c.missed} missed`, `${c.unmeasured} unmeasured (${c.laws} laws, from GET /laws)`]
  const open = rows.map(unattributed)
  parts.push(open.includes(null) ? '? missed without a cause' : `${open.filter(Boolean).length} missed without a cause`)
  return parts.join(' · ')
}

/** The page's word: any missed is warn; all reached is ok; otherwise unknown. From GET /laws' counts. */
export function auditWord(laws: readonly RegisterLaw[] | null): 'ok' | 'warn' | 'unknown' {
  if (laws == null) return 'unknown'
  const c = lawCounts(laws)
  if (c.missed > 0) return 'warn'
  if (c.reached > 0 && c.unmeasured === 0) return 'ok'
  return 'unknown'
}

export const CAUSE_UNREAD = 'Cause not read yet: this step’s events are not read'
export const CAUSE_NONE = 'The audit has not attributed this missed standard yet'
const CAUSE_WORD: Record<Attribution['category'], string> = { law: 'law', agent: 'agent', tool: 'tool', unchecked: 'not checked', unreadable: 'unknown' }
const SOURCE_WORD: Record<Reading['source'], string> = { archived: 'kept by the system', 'ad hoc': 'read once', mixed: 'some read once', unsaid: 'not said' }

function causeCell(r: StandardRow): Cell {
  if (r.result !== 'missed') return '—'
  const a = r.attribution
  if (a === undefined) return { text: 'unknown', tone: 'unknown', title: CAUSE_UNREAD }
  if (a === null) return { text: 'not attributed', tone: 'unknown', title: CAUSE_NONE }
  const word = CAUSE_WORD[a.category]
  return { text: word, ...(a.category === 'unchecked' || a.category === 'unreadable' ? { tone: 'unknown' as const } : {}), title: [a.reason, a.to ? '→ ' + a.to : '', a.raw ? `(not readable: ${a.raw})` : ''].filter(Boolean).join(' ') }
}

const short = (t: string, n = 18) => (t.length > n ? t.slice(0, n) + '…' : t)

function numbersCell(r: StandardRow): Cell {
  if (r.reading === undefined) return { text: '…', title: 'Reading this step’s events' }
  if (r.reading === null) return { text: '—', title: 'No verdict posted on this step' }
  if (!r.reading.found) return { text: 'not found', tone: 'unknown', title: `Note ${r.reading.noteEventId} is not among the step’s last 50 events` }
  // A number read once is never drawn as a conclusion (the Owner, 11-19): it says so.
  return { text: SOURCE_WORD[r.reading.source], ...(r.reading.source === 'archived' ? {} : { tone: 'unknown' as const }) }
}

/** The conclusions as a Ledger table: law and standard, result, where its numbers come from, cause, plan step. */
export function auditTable(rows: readonly StandardRow[]): Extract<Block, { kind: 'table' }> {
  return {
    kind: 'table',
    head: [{ label: 'Law · standard' }, { label: 'Result' }, { label: 'Numbers' }, { label: 'Cause' }, { label: 'Plan step' }],
    rows: ordered(rows).map(r => ({
      key: r.step,
      // The first cell opens the row (the Ledger opens a row by its first cell); the law leads it, so rows read grouped.
      open: r.step,
      cells: [
        // Short, so the result stays in view on a phone (390 check, 2026-11-24); the whole standard is in the title and the detail.
        { text: `${r.law} ${r.label ? r.label + ' · ' : '· '}${short(r.standard)}`, title: `Law ${r.law}: ${r.standard}` },
        { text: r.result, tone: TONE[r.result], title: r.grade ? `${r.grade}${r.card ? ' · standard version ' + r.card : ''}` : 'no verdict yet' },
        numbersCell(r),
        causeCell(r),
        r.step,
      ],
    })),
  }
}

/** The next fillers' list: every unmeasured standard and what it still lacks, as its plan step says. */
export function unmeasuredList(rows: readonly StandardRow[]): { key: string; what: string; lacks: string; href: string }[] {
  return ordered(rows).filter(r => r.result === 'unmeasured').map(r => ({
    key: r.step, what: `Law ${r.law} ${r.label || r.step}`, lacks: r.now || 'not written on the step', href: planNodeHref(r.step),
  }))
}

/** References written in a reading: approval #n, evidence_id n, run names, commit ids. Listed as written, nothing guessed. */
export function refsOf(text: string): string[] {
  const out = new Set<string>()
  for (const m of text.matchAll(/#\d+/g)) out.add('approval ' + m[0])
  for (const m of text.matchAll(/evidence_id\s*[:：]?\s*(\d+)/g)) out.add('evidence_id ' + m[1])
  for (const m of text.matchAll(/\b(?:ci|land)-[a-z0-9-]+/g)) out.add('run ' + m[0])
  for (const m of text.matchAll(/(?<![_a-z0-9])[0-9a-f]{8,40}(?![0-9a-z])/g)) out.add('commit ' + m[0])
  return [...out]
}

/** A row opened: the conclusion and the cause apart, then the evidence and where to go. */
export function auditDetail(r: StandardRow | undefined, extra?: { label: string; text: string }): Block[] | null {
  if (!r) return null
  const reading = r.reading === undefined ? 'not read yet' : r.reading === null ? 'no verdict posted on this step' : r.reading.found ? r.reading.text : `note ${r.reading.noteEventId} is not among the step’s last 50 events`
  const a = r.attribution
  const conclusion: [string, string][] = [
    ['Result', `${r.result}${r.grade ? ' (' + r.grade + ')' : ''}`],
    ['Standard', r.standard],
    ['Standard version', r.card ?? 'not recorded'],
    ['How it is measured', r.method || 'not written'],
    ['Where it stands', r.now || 'not written'],
    ['Reading', reading],
    ['Numbers', r.reading?.found ? SOURCE_WORD[r.reading.source] : '—'],
    ...(extra ? [[extra.label, extra.text] as [string, string]] : []),
  ]
  const cause: [string, string][] = r.result !== 'missed' ? [['Cause', 'only a missed standard has one']]
    : a === undefined ? [['Cause', CAUSE_UNREAD]]
      : a === null ? [['Cause', CAUSE_NONE]]
        : [['Category', CAUSE_WORD[a.category]], ['Reason', a.reason || 'not written'], ['Goes to', a.to || 'not written'], ['Read from', a.from === 'event' ? 'plan_step_attributed (GET /laws)' : `note ${a.noteEventId ?? ''} (text)`], ...(a.raw ? [['As written', a.raw] as [string, string]] : [])]
  const refs = r.reading?.found ? refsOf(r.reading.text) : []
  const table = (title: string, facts: [string, string][]): Block => ({ kind: 'table', head: [{ label: title }, { label: '' }], rows: facts.map(([k, v]) => ({ key: title + k, cells: [k, v] })) })
  return [
    table('Conclusion', conclusion),
    table('Cause', cause),
    { kind: 'table', head: [{ label: 'Evidence' }], rows: [
      { key: 'step', cells: [`Plan step ${r.step}`], href: planNodeHref(r.step) },
      ...(r.reading ? [{ key: 'judged', cells: [`Verdict event ${r.reading.judgedEventId} · reading note ${r.reading.noteEventId}`] }] : []),
      ...refs.map(x => ({ key: 'ref:' + x, cells: [x] })),
      ...(r.planItem ? [{ key: 'item', cells: [`Plan item ${r.planItem}`], href: planNodeHref(r.planItem) }] : []),
      { key: 'law', cells: [`Law ${r.law} in the register`], href: lawHref(r.law) },
    ] },
  ]
}

/**
 * Law 85 s1 (the audit line): the daily cards pushed to the Owner, per day, newest first; only the days since the law
 * was said count (its said_on from GET /laws; the cards before it declared no class).
 */
export type ApprovalReadings = { days?: { day: string; to_owner?: { daily?: unknown[] } }[] }
export function dailyCards(r: ApprovalReadings | undefined, from: string | undefined): string {
  if (!r?.days) return 'not read'
  if (!from) return 'the law’s date is not read, so the days that count are not known'
  const days = r.days.filter(d => d.day >= from.slice(0, 10))
  if (days.length === 0) return `no day since ${from} yet`
  return days.map(d => `${d.day}: ${d.to_owner?.daily?.length ?? 0} daily cards`).join(' · ') + ` (GET /api/v1/readings/approvals, kept by the system; days since ${from})`
}
