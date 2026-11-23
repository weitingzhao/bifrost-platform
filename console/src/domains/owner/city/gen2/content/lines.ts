import type { Block, Cell, Tone } from '@bifrost/ui/city/cards'
import type { Panel } from '@bifrost/ui/city/content'
import type { LineageCommit, LineageResponse, LineageThread } from '@/api/lineage'

/**
 * What each line did, on the City (the chief of staff's brief, 2026-11-22: the Owner sees in the City as in the
 * Console what every line did lately and whether it went live, without asking anyone). Everything is read from
 * the lineage read (GET /api/v1/lineage, the commit trailers on the Gitea mirror and the recorded releases):
 *   merged    the thread's commits on main (the read's own `landed`)
 *   STG/PROD  its commits a deploying release in that env contained (as the Console's Commit lineage counts PROD)
 *   moved     its newest commit (`last_at`)
 * over the same window the Console's Commit lineage page opens with (14 days), so the two read the same numbers.
 *
 * Which domain a line sits is the seat roster's (Crew's seats.yaml). The platform does not serve it yet: until it
 * does, the lines stand on the city level only and a district says so; nothing is matched here by hand.
 */
export const LINES_DAYS = 14
/** A line that moved within this is moving; the rest are quiet, with how long. */
export const MOVING_HOURS = 24

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

export type LineCounts = { session: string; title: string; merged: number; stg: number; prod: number; commits: number; lastAt: string }

const inEnv = (c: LineageCommit, env: string) => (c.reached ?? []).some(r => r.deploys && r.env === env)

export function countsOf(t: LineageThread): LineCounts {
  const all = t.repos.flatMap(r => r.commits)
  return {
    session: t.session,
    title: t.title || (t.session ? t.session : 'no session trailer'),
    merged: t.landed,
    stg: all.filter(c => inEnv(c, 'stg')).length,
    prod: all.filter(c => inEnv(c, 'prod')).length,
    commits: t.commit_count,
    lastAt: t.last_at,
  }
}

/** How long since a line moved, and whether that reads as moving (good) or quiet (not known: no judgment of why). */
export function pace(lastAt: string, now: Date): { text: string; tone: Tone } {
  const hours = (now.getTime() - Date.parse(lastAt)) / 3_600_000
  if (!Number.isFinite(hours)) return { text: 'unknown', tone: 'unknown' }
  if (hours < 1) return { text: `${Math.max(0, Math.round(hours * 60))} min ago`, tone: 'good' }
  if (hours < MOVING_HOURS) return { text: `${Math.round(hours)} h ago`, tone: 'good' }
  return { text: `quiet ${Math.floor(hours / 24)} d`, tone: 'unknown' }
}

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

function table(rows: LineCounts[], now: Date, domainCol?: (r: LineCounts) => Cell): Block {
  return {
    kind: 'table',
    head: [{ label: 'Line' }, { label: 'Moved' }, ...(domainCol ? [{ label: 'Domain' }] : []), { label: 'Merged', num: true }, { label: 'STG', num: true }, { label: 'PROD', num: true }],
    empty: `No line committed in the last ${LINES_DAYS} days`,
    rows: [...rows].sort((a, b) => b.lastAt.localeCompare(a.lastAt)).map(r => {
      const p = pace(r.lastAt, now)
      return {
        key: r.session || 'none',
        title: `${r.commits} commits in ${LINES_DAYS} days; ${r.commits - r.merged} not on main`,
        ghost: p.tone !== 'good',
        cells: [{ text: r.title, title: r.session || undefined }, p, ...(domainCol ? [domainCol(r)] : []), r.merged, r.stg, r.prod],
      }
    }),
  }
}

const SOURCE = `From the lineage read (commit trailers on the Gitea mirror, recorded releases), the last ${LINES_DAYS} days, as the Console's Commit lineage counts them. STG and PROD: commits a deploying release in that environment contained. Moving: a commit in the last ${MOVING_HOURS} h.`
const NO_ROSTER = 'Which domain each line sits is not read: the seat roster is not served by the platform yet (asked of Crew; the map line serves it).'

/** The city level's card: every line, the moving ones first, with what it merged and how far it went. */
export function townLines(read: LinesRead, roster: RosterRead, now: Date): Block[] {
  if (!read.data) return [{ kind: 'note', text: `What the lines did is not known: ${whyOf(read.error)}` }]
  const rows = read.data.threads.map(countsOf)
  const moving = rows.filter(r => pace(r.lastAt, now).tone === 'good').length
  const seats = roster.data
  const col = seats ? (r: LineCounts): Cell => {
    const ds = domainsOf(seats, r.session)
    return ds.length > 0 ? { lines: ds } : { text: 'no seat', tone: 'unknown' }
  } : undefined
  return [
    { kind: 'facts', rows: [{ label: 'Moving', value: { text: moving, tone: 'good' }, note: `committed in the last ${MOVING_HOURS} h` }, { label: 'Quiet', value: rows.length - moving, note: `of ${rows.length} lines with commits in ${LINES_DAYS} days` }] },
    table(rows, now, col),
    { kind: 'note', text: seats ? SOURCE : `${SOURCE} ${NO_ROSTER}` },
  ]
}

/** A district's card: the lines that sit its domain, by the roster; said plainly when the roster is not read. */
export function districtLines(read: LinesRead, roster: RosterRead, id: string, now: Date): Block[] {
  if (!roster.data) return [{ kind: 'note', text: `${NO_ROSTER}${roster.error ? ` (${whyOf(roster.error)})` : ''} The lines are on the city level.` }]
  if (!read.data) return [{ kind: 'note', text: `What the lines did is not known: ${whyOf(read.error)}` }]
  const sessions = new Set(roster.data.filter(s => s.session && (s.domain_id === id || (s.also_domain_ids ?? []).includes(id))).map(s => s.session!))
  if (sessions.size === 0) return [{ kind: 'note', text: 'No seat in the roster sits this domain.' }]
  const rows = read.data.threads.filter(t => sessions.has(t.session)).map(countsOf)
  const silent = [...sessions].filter(s => !rows.some(r => r.session === s))
  return [
    table(rows, now),
    ...(silent.length > 0 ? [{ kind: 'note' as const, text: `${silent.length} seat${silent.length === 1 ? '' : 's'} of this domain committed nothing in ${LINES_DAYS} days.` }] : []),
    { kind: 'note', text: SOURCE },
  ]
}

export const linesPanel = (read: LinesRead, roster: RosterRead, now: Date): Panel =>
  ({ id: 'lines', title: `Lines · what each did (${LINES_DAYS} d)`, dock: 'right', cards: townLines(read, roster, now) })

export const districtLinesPanel = (read: LinesRead, roster: RosterRead, id: string, now: Date): Panel =>
  ({ id: 'lines', title: 'Lines in this domain', dock: 'right', cards: districtLines(read, roster, id, now) })
