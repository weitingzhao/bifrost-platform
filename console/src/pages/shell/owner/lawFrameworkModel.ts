import type { FrameworkField, FrameworkLevel, LawFramework, PurgeQueue, RegisterLaw } from '@/api/lawRegister'

/*
 * The framework of laws on Command → Laws (Owner 2026-11-24 by the legislation line: "我要在 console 的那个里面有一个入口
 * 能看到就可以了"; step one is a frame that can hold laws, shown with no law in it). Five blocks: the five levels, the
 * fields, the rules between laws and what refuses a card, the states' counts, and the material and repealed laws
 * behind a switch. What the platform does not send is "unknown", never filled in.
 */

export const IN_FORCE = '现行'
export const MATERIAL = '素材'
export const REPEALED = '废止'
/** The states in the framework's order; a state the platform adds later is shown after them. */
export const STATE_ORDER = [IN_FORCE, MATERIAL, REPEALED] as const

export const UNKNOWN = 'unknown'

/** "v1 · 5 levels · 现行 0 · 素材 85 · 废止 0"; a framework not sent says so. */
export function frameworkLine(fw: LawFramework | null | undefined): string {
  if (!fw) return 'framework not sent by this platform: unknown'
  const levels = fw.levels ? `${fw.levels.length} levels` : `levels ${UNKNOWN}`
  const states = stateCounts(fw).map(([s, n]) => `${s} ${n ?? '?'}`).join(' · ')
  return [`framework ${fw.version ?? UNKNOWN}`, levels, states].join(' · ')
}

/**
 * Each state with its count, in the framework's order; a state with no count is null (shown "?"), never 0. The repealed
 * are not a state the register keeps any more (立法线 2026-11-24, after the Owner: "删掉..对于法律来说都是毒药"): 废止 is
 * shown only while the platform still sends it, never as "废止 0" or unknown.
 */
export function stateCounts(fw: LawFramework | null | undefined): [string, number | null][] {
  const states = fw?.states
  const shown = STATE_ORDER.filter(s => s !== REPEALED || (states != null && Object.hasOwn(states, REPEALED)))
  const known = shown.map(s => [s, states && typeof states[s] === 'number' ? states[s] : null] as [string, number | null])
  const more = Object.entries(states ?? {}).filter(([s]) => !(STATE_ORDER as readonly string[]).includes(s))
  return [...known, ...more]
}

/** A required field by its label (the framework's own words); a name the field list does not have stays its name. */
export function requiredLabels(level: FrameworkLevel, fields: readonly FrameworkField[] | undefined): string[] {
  const label = new Map((fields ?? []).map(f => [f.name, f.label]))
  return (level.required ?? []).map(n => label.get(n) ?? n)
}

/** The laws in force at one level: state 现行 and framed at that level. */
export const inForceAt = (laws: readonly RegisterLaw[], level: number): RegisterLaw[] =>
  laws.filter(l => l.state === IN_FORCE && l.frame?.level === level)

/** The laws behind the switch: material, repealed, and any law whose state the platform did not say. */
export const behindSwitch = (laws: readonly RegisterLaw[]): RegisterLaw[] => laws.filter(l => l.state !== IN_FORCE)

/** The page's word: the framework read is ok; not sent or not read is unknown. */
export const frameworkWord = (fw: LawFramework | null | undefined): 'ok' | 'unknown' => (fw && fw.levels ? 'ok' : 'unknown')

/**
 * The register's entries counted by state (立法线 2026-11-24, after the Owner read "85 laws" as 85 laws in the
 * framework): only 现行 is a law; 素材 is material, 废止 repealed. A row without a state is counted as unknown.
 */
export function stateTally(laws: readonly RegisterLaw[]): string {
  const n = (s: string) => laws.filter(l => l.state === s).length
  const unknown = laws.filter(l => !l.state || !(STATE_ORDER as readonly string[]).includes(l.state)).length
  const shown = STATE_ORDER.filter(s => s !== REPEALED || n(s) > 0)
  return [...shown.map(s => `${s} ${n(s)}`), ...(unknown > 0 ? [`state unknown ${unknown}`] : [])].join(' · ')
}
export const inForceCount = (laws: readonly RegisterLaw[]): number => laws.filter(l => l.state === IN_FORCE).length

/**
 * Step two (Owner 2026-11-24): the material is distilled into laws in force one by one; once every piece is absorbed
 * the material is repealed in one go. The platform counts it (framework.coverage); this only says it.
 */
export function coverageLine(fw: LawFramework | null | undefined): string {
  const c = fw?.coverage
  if (!c) return `material covered: ${UNKNOWN}`
  return `material covered ${c.covered} / ${c.material}` + (c.material > 0 && c.covered === c.material ? ' · all covered: ready to repeal the material' : '')
}

/** The material rows first those no law absorbs yet, then the absorbed; each group by number. */
export function materialOrder(laws: readonly RegisterLaw[]): RegisterLaw[] {
  const num = (l: RegisterLaw) => Number(l.law) || 0
  const open = (l: RegisterLaw) => (l.state === MATERIAL && !(l.absorbed_by && l.absorbed_by.length > 0) ? 0 : 1)
  return [...laws].sort((a, b) => open(a) - open(b) || num(a) - num(b))
}

/**
 * "awaiting deletion 16 · overdue 0 · next 2026-12-01" (LAW-1 s5: a repealed law is deleted seven days on). Not sent by
 * the platform says so; nothing waiting says so.
 */
export function purgeLine(q: PurgeQueue | null | undefined): string {
  if (!q) return `repealed awaiting deletion: ${UNKNOWN}`
  if (q.waiting === 0) return 'repealed awaiting deletion 0'
  const next = q.next_purge_at ? ` · next ${q.next_purge_at.slice(0, 10)}` : ''
  return `repealed awaiting deletion ${q.waiting} · overdue ${q.overdue}${next}`
}

