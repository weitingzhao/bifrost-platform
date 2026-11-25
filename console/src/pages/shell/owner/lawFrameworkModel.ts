import type { FrameworkField, FrameworkLevel, LawFramework, PurgeQueue, RegisterLaw } from '@/api/lawRegister'

/*
 * The framework of laws on Command → Laws (ISSUE-17: one entry in the Console is enough). The blocks: the levels with
 * the laws in force under them, the fields, the rules between laws and what refuses a card, and the count in force.
 * The register holds laws only (LAW-1 四, ISSUE-104): two states, 现行 and 废止, and the page draws only 现行.
 * What the platform does not send is "unknown", never filled in.
 */

export const IN_FORCE = '现行'
export const REPEALED = '废止'
/** The states in the framework's order; a state the platform adds later is shown after them. */
export const STATE_ORDER = [IN_FORCE, REPEALED] as const

export const UNKNOWN = 'unknown'

/** "framework v1 · 4 levels · 现行 22"; a framework not sent says so. */
export function frameworkLine(fw: LawFramework | null | undefined): string {
  if (!fw) return 'framework not sent by this platform: unknown'
  const levels = fw.levels ? `${fw.levels.length} levels` : `levels ${UNKNOWN}`
  const states = stateCounts(fw).map(([s, n]) => `${s} ${n ?? '?'}`).join(' · ')
  return [`framework ${fw.version ?? UNKNOWN}`, levels, states].join(' · ')
}

/**
 * Each state with its count, in the framework's order; a state with no count is null (shown "?"), never 0. The repealed
 * are not a state the register keeps any more (立法线 2026-11-24; ISSUE-24): 废止 is
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

/** The laws in force that lack a level (LAW-1 四: a law without its frame is a law with fields missing, said so). */
export const inForceWithoutLevel = (laws: readonly RegisterLaw[]): RegisterLaw[] =>
  laws.filter(l => l.state === IN_FORCE && typeof l.frame?.level !== 'number')

/** The laws in force, by register number: the only rows the page draws. */
export const inForceLaws = (laws: readonly RegisterLaw[]): RegisterLaw[] =>
  laws.filter(l => l.state === IN_FORCE).sort((a, b) => (Number(a.law) || 0) - (Number(b.law) || 0))

/** The page's word: the framework read is ok; not sent or not read is unknown. */
export const frameworkWord = (fw: LawFramework | null | undefined): 'ok' | 'unknown' => (fw && fw.levels ? 'ok' : 'unknown')

/**
 * The register's entries counted by state (立法线 2026-11-24, after "85 laws" was read as 85 laws in the framework):
 * 现行, and 废止 only while the platform still sends such rows. A row without a state is counted as unknown.
 */
export function stateTally(laws: readonly RegisterLaw[]): string {
  const n = (s: string) => laws.filter(l => l.state === s).length
  const unknown = laws.filter(l => !l.state || !(STATE_ORDER as readonly string[]).includes(l.state)).length
  const shown = STATE_ORDER.filter(s => s !== REPEALED || n(s) > 0)
  return [...shown.map(s => `${s} ${n(s)}`), ...(unknown > 0 ? [`state unknown ${unknown}`] : [])].join(' · ')
}
export const inForceCount = (laws: readonly RegisterLaw[]): number => laws.filter(l => l.state === IN_FORCE).length

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

