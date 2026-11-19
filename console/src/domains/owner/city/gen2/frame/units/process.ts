/**
 * How a unit is made (the Owner, 2026-11-19): from the inside out, one stage at a time, each stage
 * shown on a page and confirmed by the Owner before the next starts. The system knows many parameters
 * the viewer cannot see, so the detail comes first, in full; every stage after it summarises — an
 * outer stage shows nothing the inner one lacks. RULES.md, 由内向外.
 *
 * A unit is one resource of a device (CPU, memory, disk, a volume, GPU, a network port). Its spec is
 * data (content/units/*.ts); this file says what the stages are, what each makes, what the Owner
 * confirms at each, and what the machine checks — the same for every unit.
 */

import { DICTIONARY } from '../library/dictionary'

export const STAGES = [
  {
    id: 'details', name: 'Details',
    makes: 'Every parameter the system has for this resource: its query, its unit, what it is per (machine, core, sensor…), read on every machine of the district.',
    confirms: 'Which of them the room shows.',
    checks: ['every detail has a query and a unit', 'every machine is read: a value, or plainly not read'],
  },
  {
    id: 'inside', name: 'Inside',
    makes: 'The room: every chosen detail drawn at full size, each through a row of the metaphor dictionary, with its key, held to the room standards (frame/units/standards.ts), its console among them.',
    confirms: 'Whether the room shows each detail naturally.',
    checks: ['every chosen detail is drawn', 'every drawn thing has a dictionary row', 'the room standards marked tested'],
  },
  {
    id: 'summary', name: 'Summary',
    makes: 'What the unit passes up to its building: each item made from which details, and how (average, maximum, total, count, or the worst state). It is what the room\'s console shows — the room sums itself up there, and the same goes outside.',
    confirms: 'Whether the summary keeps what matters.',
    checks: ['every item is made from chosen details', 'every item says how it is made', 'every item shows through a dictionary row'],
  },
  {
    id: 'outside', name: 'Outside',
    makes: 'Its miniature on a building, drawn by the same drawing as the room, smaller: it shows the summary and nothing else.',
    confirms: 'Whether it reads at a glance from the street.',
    checks: ['it draws only rows of the summary'],
  },
  {
    id: 'in-use', name: 'In use',
    makes: 'Buildings assembled from it (their layouts), on the map, with live readings.',
    confirms: 'That it is done; a change to it changes every building that uses it.',
    checks: ['every building that has this resource uses this unit, and no other drawing of it'],
  },
] as const

export type StageId = (typeof STAGES)[number]['id']

/**
 * What a detail is measured over (the Owner, 2026-11-19: each reading drawn where it belongs, or the
 * picture is a concept drawing): the whole unit (one value), each core, each sensor (a sensor says by
 * its label whether it measures the whole chip or one core), or each mode of time. The room draws a
 * per-core reading on every machine and a whole-CPU reading once, on the workshop.
 */
export type Per = 'unit' | 'core' | 'sensor' | 'mode' | 'disk' | 'volume' | 'array'

export type Detail = {
  id: string
  label: string
  /** What it says, in a viewer's words. */
  meaning: string
  /** The PromQL that reads it, labelled by `instance` (node exporter). */
  query: string
  unit: 'share' | 'count' | 'per second' | 'Hz' | '°C' | 'load' | 'bytes' | 'bytes per second' | 'seconds' | 'rpm' | 'kind'
  per: Per
  /**
   * Who knows it (room standard 1: only what the hardware itself knows goes in the room): the hardware's
   * own state, or an event the operating system counts. The Owner picks at Details; this is the advice.
   */
  known?: 'hardware' | 'system'
}

/**
 * Inside, drafted: what each chosen detail becomes in the room — a piece of the facility's own
 * equipment — and how its reading shows on it. Drafted before it is drawn, so the Owner sees the
 * mapping first; once drawn, each item is a row of the metaphor dictionary.
 */
export type InsideItem = { detail: string; thing: string; shows: string; where: string }

export type Aggregate = 'average' | 'maximum' | 'total' | 'count' | 'worst state'

export type SummaryItem = { id: string; label: string; from: string[]; how: Aggregate; row: string }

/** An Owner's confirmation of a stage, as he said it in the thread: the day, and his words. */
export type Confirmed = { on: string; words: string }

export type UnitSpec = {
  id: string
  name: string
  /** What resource it is, and its city metaphor. */
  what: string
  details: Detail[]
  /** The details the room shows; set when the Owner confirms Details. */
  chosen?: string[]
  inside?: InsideItem[]
  summary?: SummaryItem[]
  confirmed: Partial<Record<StageId, Confirmed>>
}

export type StageState = 'confirmed' | 'open' | 'waiting'

/** Where each stage stands: confirmed by the Owner; open (the one being made now); waiting on an earlier one. */
export function stagesOf(u: UnitSpec): { id: StageId; state: StageState }[] {
  let open = false
  return STAGES.map(s => {
    if (u.confirmed[s.id] && !open) return { id: s.id, state: 'confirmed' as const }
    if (!open) { open = true; return { id: s.id, state: 'open' as const } }
    return { id: s.id, state: 'waiting' as const }
  })
}

/** The machine's checks on a spec, the same for every unit. Empty when it holds. */
export function unitRulesBroken(u: UnitSpec): string[] {
  const out: string[] = []
  const ids = new Set(u.details.map(d => d.id))
  if (ids.size !== u.details.length) out.push(`${u.id}: two details share an id`)
  for (const d of u.details) if (!d.query.trim() || !d.unit) out.push(`${u.id}.${d.id}: a detail needs a query and a unit`)
  // Confirmed only in order: a later stage cannot be confirmed over an earlier one.
  let gap = false
  for (const s of STAGES) {
    if (!u.confirmed[s.id]) gap = true
    else if (gap) out.push(`${u.id}: ${s.name} is confirmed before an earlier stage`)
  }
  if (u.confirmed.details && !u.chosen?.length) out.push(`${u.id}: Details is confirmed but no detail is chosen`)
  for (const c of u.chosen ?? []) if (!ids.has(c)) out.push(`${u.id}: chosen "${c}" is not a detail`)
  for (const it of u.inside ?? []) if (!(u.chosen ?? []).includes(it.detail)) out.push(`${u.id}: the room draws "${it.detail}", which is not chosen`)
  if (u.inside) for (const c of u.chosen ?? []) if (!u.inside.some(it => it.detail === c)) out.push(`${u.id}: chosen "${c}" has no place in the room`)
  for (const it of u.summary ?? []) {
    if (it.from.length === 0) out.push(`${u.id}.${it.id}: a summary item is made from no detail`)
    for (const f of it.from) if (!(u.chosen ?? []).includes(f)) out.push(`${u.id}.${it.id}: made from "${f}", which the room does not show`)
    if (!DICTIONARY.some(m => m.id === it.row)) out.push(`${u.id}.${it.id}: shows through "${it.row}", which the metaphor dictionary does not have`)
  }
  return out
}
