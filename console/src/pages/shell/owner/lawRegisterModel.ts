import type { Block, Cell, Word } from '@bifrost/ui/city/cards'
import type { RegisterLaw } from '@/api/lawRegister'
import { formatShellHash } from '@/lib/shell/consoleRoutes'

/*
 * The register of laws on Command (箭10 2026-11-24): every law by number with its text, status, when it was said, what
 * enforces it, and how many of its standards are reached, missed and unmeasured. The words are the Ops vocabulary:
 * reached is ok, missed (partly or not reached) is warn, unmeasured (not checked, no verdict) is unknown, never 0.
 * A law no approved card has changed has no plan item and no numbers: it says so, it is not counted as reached.
 */

/** A step's grade in the vocabulary: 达到 ok; 部分, 没达到 warn; 没核 or no verdict unknown. */
export function gradeTone(grade: string | undefined): 'ok' | 'warn' | 'unknown' {
  const g = grade?.trim() ?? ''
  if (g === '达到') return 'ok'
  if (g === '部分' || g === '没达到') return 'warn'
  return 'unknown'
}

const GRADE_WORD: Record<'ok' | 'warn' | 'unknown', string> = { ok: 'reached', warn: 'missed', unknown: 'unmeasured' }

/**
 * A law's own state, its worst standard deciding (箭10 2026-11-24, as every rollup here): any standard missed is
 * missed; else any unmeasured is unmeasured; all reached is reached. A law with no counts has no plan item.
 */
export function lawState(l: Pick<RegisterLaw, 'counts'>): 'reached' | 'missed' | 'unmeasured' | 'no-item' {
  const c = l.counts
  if (c == null) return 'no-item'
  if (c.missed > 0) return 'missed'
  if (c.unmeasured > 0 || c.reached === 0) return 'unmeasured'
  return 'reached'
}

/** The page's one-line verdict, laws counted by their state: "83 laws · 12 reached · 3 missed · 7 unmeasured · 61 without a plan item". */
export function lawRegisterVerdict(laws: readonly RegisterLaw[] | null): string {
  if (laws == null) return '? laws · ? reached · ? missed · ? unmeasured'
  const by = (k: ReturnType<typeof lawState>) => laws.filter(l => lawState(l) === k).length
  const parts = [`${laws.length} laws`, `${by('reached')} reached`, `${by('missed')} missed`, `${by('unmeasured')} unmeasured`]
  const without = by('no-item')
  if (without > 0) parts.push(`${without} without a plan item`)
  return parts.join(' · ')
}

/** The standards summed, for the hover: "standards: 40 reached · 5 missed · 60 unmeasured". */
export function lawStandardsLine(laws: readonly RegisterLaw[] | null): string {
  if (laws == null) return 'standards: not read'
  const sum = (k: 'reached' | 'missed' | 'unmeasured') => laws.reduce((n, l) => n + (l.counts?.[k] ?? 0), 0)
  return `standards: ${sum('reached')} reached · ${sum('missed')} missed · ${sum('unmeasured')} unmeasured`
}

/** The register on Command. */
export const LAWS_HREF = formatShellHash('command', 'section=laws')

/** A law's address on Command: the register, that law picked. */
export const lawHref = (law: string) => formatShellHash('command', new URLSearchParams({ section: 'laws', law }).toString())

/** A plan node on Work's plan. */
export const planNodeHref = (node: string) => formatShellHash('work', new URLSearchParams({ node }).toString())

const count = (n: number | undefined, tone: 'ok' | 'warn' | 'unknown', has: boolean): Word =>
  !has ? { text: '—', title: 'No approved card has changed this law yet: no plan item, no standards to count' }
    : { text: n ?? 0, ...(n ? { tone } : {}) }

/** The register as a Ledger table, one row per law; the counts are numbers so a sort orders them. */
export function lawRegisterTable(laws: readonly RegisterLaw[]): Extract<Block, { kind: 'table' }> {
  return {
    kind: 'table',
    head: [
      { label: 'Law', num: true }, { label: 'Text' }, { label: 'Status' }, { label: 'Said on' }, { label: 'Enforced by' },
      { label: 'Reached', num: true }, { label: 'Missed', num: true }, { label: 'Unmeasured', num: true }, { label: 'State' }, { label: 'Absorbed by' },
    ],
    rows: laws.map(l => {
      const has = l.counts != null
      const cells: Cell[] = [
        l.law,
        { text: l.text, title: l.where ? `${l.text}\n(${l.where})` : l.text },
        l.status,
        l.said_on ?? '—',
        l.enforced_by || '—',
        count(l.counts?.reached, 'ok', has),
        count(l.counts?.missed, 'warn', has),
        count(l.counts?.unmeasured, 'unknown', has),
        l.state ?? { text: 'unknown', title: 'The platform did not say this law\'s state' },
        // Step two (Owner 2026-11-24): which law in force absorbs this material; "none" until one does.
        l.absorbed_by && l.absorbed_by.length > 0 ? l.absorbed_by.map(n => `Law ${n}`).join(', ')
          : l.state === '素材' ? { text: 'none', title: 'No law in force absorbs this material' } : '—',
      ]
      return { key: l.law, cells }
    }),
  }
}

/** A picked law, opened: its standards' steps, each with its grade, and its plan item. */
export function lawDetail(law: RegisterLaw | undefined): Block[] | null {
  if (!law) return null
  const blocks: Block[] = []
  if (law.plan_item) {
    blocks.push({ kind: 'table', head: [{ label: 'Plan item' }], rows: [{ key: law.plan_item, cells: [law.plan_item], href: planNodeHref(law.plan_item) }] })
  }
  if (law.steps && law.steps.length > 0) {
    blocks.push({
      kind: 'table',
      head: [{ label: 'Step' }, { label: 'Title' }, { label: 'Grade' }],
      rows: law.steps.map(s => {
        const tone = gradeTone(s.grade)
        return { key: s.plan_node_id, cells: [s.plan_node_id, s.title, { text: GRADE_WORD[tone], tone, title: s.grade || 'no verdict yet' }], href: planNodeHref(s.plan_node_id) }
      }),
    })
  }
  if (blocks.length === 0) return [{ kind: 'note', text: 'No approved card has changed this law yet: no plan item and no standards to show.' }]
  return blocks
}

/** The register's word: missed anywhere is warn; all counted standards reached is ok; otherwise unknown (not read, or nothing measured yet). */
export function lawRegisterWord(laws: readonly RegisterLaw[] | null): 'ok' | 'warn' | 'unknown' | 'pending' {
  if (laws == null) return 'unknown'
  const counted = laws.filter(l => l.counts != null)
  if (counted.some(l => (l.counts?.missed ?? 0) > 0)) return 'warn'
  if (counted.length > 0 && counted.every(l => (l.counts?.unmeasured ?? 0) === 0)) return 'ok'
  return 'unknown'
}
