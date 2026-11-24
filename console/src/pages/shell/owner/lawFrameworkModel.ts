import type { FrameworkField, FrameworkLevel, LawFramework, RegisterLaw } from '@/api/lawRegister'

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

/** Each state with its count, in the framework's order; a state with no count is null (shown "?"), never 0. */
export function stateCounts(fw: LawFramework | null | undefined): [string, number | null][] {
  const states = fw?.states
  const known = STATE_ORDER.map(s => [s, states && typeof states[s] === 'number' ? states[s] : null] as [string, number | null])
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
  return [...STATE_ORDER.map(s => `${s} ${n(s)}`), ...(unknown > 0 ? [`state unknown ${unknown}`] : [])].join(' · ')
}
export const inForceCount = (laws: readonly RegisterLaw[]): number => laws.filter(l => l.state === IN_FORCE).length
