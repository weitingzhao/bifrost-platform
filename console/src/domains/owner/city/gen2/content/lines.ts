import type { Block, Cell, Tone } from '@bifrost/ui/city/cards'
import type { Panel } from '@bifrost/ui/city/content'
import type { LineageResponse } from '@/api/lineage'
import { linesActivity, quietLabel, type LineActivity } from '@/api/lineageActivity'

/**
 * What each line did, on the City (the chief of staff's brief, 2026-11-22: the Owner sees in the City as in the
 * Console what every line did lately and whether it went live, without asking anyone). Read from the lineage read
 * (GET /api/v1/lineage) and counted by `linesActivity`, the one function the Console's Commit lineage counts with,
 * over the same window it opens with (14 days): one number on both sides.
 *
 * Coming from the Console with a line picked (`sel=<session>` in the address), that line's row comes first, marked.
 *
 * Which domain a line sits is the seat roster's (Crew's seats.yaml). The platform does not serve it yet: until it
 * does, the lines stand on the city level only and a district says so; nothing is matched here by hand.
 */
export const LINES_DAYS = 14

/** One row of the seat roster, in the shape Crew fixed (2026-11-23, m_01M84GG2…); served by the map's scan. */
export type RosterSeat = {
  id: string
  name?: string
  kind: string
  harness?: string
  session?: string
  run_key?: string
  domain_id: string | null
  also_domain_ids?: string[]
  ended?: string | null
}
export type LinesRead = { data?: LineageResponse; error?: unknown }
export type RosterRead = { data?: RosterSeat[]; error?: unknown }

/** How long since a line moved: moving (good) or quiet (not known: no judgment of why). */
export const paceOf = (l: LineActivity): { text: string; tone: Tone } =>
  l.quietHours == null ? { text: 'unknown', tone: 'unknown' } : { text: quietLabel(l.quietHours), tone: l.movingToday ? 'good' : 'unknown' }

/** The domains a session sits, by the roster (a session may sit two seats, so two domains). */
export function domainsOf(roster: readonly RosterSeat[], session: string): string[] {
  const out = new Set<string>()
  for (const s of roster) {
    if (!session || s.session !== session) continue
    if (s.domain_id) out.add(s.domain_id)
    for (const d of s.also_domain_ids ?? []) out.add(d)
  }
  return [...out]
}

const whyOf = (e: unknown) => e instanceof Error ? e.message : e ? String(e) : 'reading'

function table(rows: LineActivity[], focus?: string, domainCol?: (r: LineActivity) => Cell): Block {
  // Newest first (linesActivity's order); the line picked in the Console first of all.
  const ordered = focus ? [...rows.filter(r => r.session === focus), ...rows.filter(r => r.session !== focus)] : rows
  return {
    kind: 'table',
    head: [{ label: 'Line' }, { label: 'Moved' }, ...(domainCol ? [{ label: 'Domain' }] : []), { label: 'Merged', num: true }, { label: 'STG', num: true }, { label: 'PROD', num: true }],
    empty: `No line committed in the last ${LINES_DAYS} days`,
    rows: ordered.map(r => ({
      key: r.session,
      title: r.session,
      ghost: !r.movingToday,
      cells: [
        { text: r.title, title: r.session, ...(r.session === focus ? { mark: { text: 'picked' } } : {}) },
        paceOf(r), ...(domainCol ? [domainCol(r)] : []), r.merged, r.stg, r.prod,
      ],
    })),
  }
}

const SOURCE = `From the lineage read (commit trailers on the Gitea mirror, recorded releases), the last ${LINES_DAYS} days, counted as the Console's Commit lineage counts them (by the furthest stage a commit reached: in PROD also counts as STG and merged). Moving: a commit today.`
const NO_ROSTER = 'Which domain each line sits is not read: the seat roster is not served by the platform yet (asked of Crew; the map line serves it).'

/** The city level's card: every line, newest first, with what it merged and how far it went. */
export function townLines(read: LinesRead, roster: RosterRead, now: Date, focus?: string): Block[] {
  if (!read.data) return [{ kind: 'note', text: `What the lines did is not known: ${whyOf(read.error)}` }]
  const rows = linesActivity(read.data.threads, now)
  const moving = rows.filter(r => r.movingToday).length
  const seats = roster.data
  const col = seats ? (r: LineActivity): Cell => {
    const ds = domainsOf(seats, r.session)
    return ds.length > 0 ? { lines: ds } : { text: 'no seat', tone: 'unknown' }
  } : undefined
  const missing = focus && focus.startsWith('local_') && !rows.some(r => r.session === focus)
  return [
    { kind: 'facts', rows: [{ label: 'Moving', value: { text: moving, tone: 'good' }, note: 'committed today' }, { label: 'Quiet', value: rows.length - moving, note: `of ${rows.length} lines with commits in ${LINES_DAYS} days` }] },
    ...(missing ? [{ kind: 'note' as const, text: `The line picked in the Console committed nothing in the last ${LINES_DAYS} days.` }] : []),
    table(rows, focus, col),
    { kind: 'note', text: seats ? SOURCE : `${SOURCE} ${NO_ROSTER}` },
  ]
}

/** A district's card: the lines that sit its domain, by the roster; said plainly when the roster is not read. */
export function districtLines(read: LinesRead, roster: RosterRead, id: string, now: Date): Block[] {
  if (!roster.data) return [{ kind: 'note', text: `${NO_ROSTER}${roster.error ? ` (${whyOf(roster.error)})` : ''} The lines are on the city level.` }]
  if (!read.data) return [{ kind: 'note', text: `What the lines did is not known: ${whyOf(read.error)}` }]
  const sessions = new Set(roster.data.filter(s => s.session && (s.domain_id === id || (s.also_domain_ids ?? []).includes(id))).map(s => s.session!))
  if (sessions.size === 0) return [{ kind: 'note', text: 'No seat in the roster sits this domain.' }]
  const rows = linesActivity(read.data.threads, now).filter(r => sessions.has(r.session))
  const silent = [...sessions].filter(s => !rows.some(r => r.session === s))
  return [
    table(rows),
    ...(silent.length > 0 ? [{ kind: 'note' as const, text: `${silent.length} seat${silent.length === 1 ? '' : 's'} of this domain committed nothing in ${LINES_DAYS} days.` }] : []),
    { kind: 'note', text: SOURCE },
  ]
}

export const linesPanel = (read: LinesRead, roster: RosterRead, now: Date, focus?: string): Panel =>
  ({ id: 'lines', title: `Lines · what each did (${LINES_DAYS} d)`, dock: 'right', cards: townLines(read, roster, now, focus) })

export const districtLinesPanel = (read: LinesRead, roster: RosterRead, id: string, now: Date): Panel =>
  ({ id: 'lines', title: 'Lines in this domain', dock: 'right', cards: districtLines(read, roster, id, now) })
