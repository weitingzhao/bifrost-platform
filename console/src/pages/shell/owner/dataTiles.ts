import type { ClusterPostgresBackupStatusResponse, DataFreshnessResponse } from '@/api/clusterTypes'
import type { OrchestrationStatusData } from '@/api/researchEngine'
import { freshnessToday } from '@/components/market-data/dataVitalsModel'
import { ago, type DataTile, type Tone } from './ownerModel'

/**
 * The five data tiles of the Owner Console: has today's data arrived, are the plugins up, did
 * the research batch run, is there a recent backup, how old are the test copies. A source that
 * cannot be read is Unknown, never OK.
 */

type Read<T> = { data: T | undefined; error: string | null }

const unknown = (name: string, href: string, why: string): DataTile => ({ name, tone: 'unknown', state: 'Unknown', figure: '—', detail: why, href })

/** `session`: undefined while it is read, null when the plugin could not say. */
export function marketTile(status: Read<{ last_run_at?: string | null }[]>, session: string | null | undefined): DataTile {
  if (status.error != null) return unknown('Market data', '#data?section=market-data', status.error)
  if (status.data == null || session === undefined) return { name: 'Market data', tone: 'muted', state: 'Loading', figure: '…', detail: '', href: '#data?section=market-data' }
  // No session from the plugin: there is no second definition of "today" to fall back on (review 221 A5).
  if (session === null) return unknown('Market data', '#data?section=market-data', 'The market-data plugin did not say which session to check')
  const today = freshnessToday(status.data, undefined, session)
  const tone: Tone = today.kind === 'ok' ? 'ok' : today.kind === 'unknown' ? 'unknown' : 'warn'
  const state = today.kind === 'ok' ? 'OK' : today.kind === 'unknown' ? 'Unknown' : today.kind === 'scheduled' ? 'Partly in' : 'Missing'
  return { name: 'Market data', tone, state, figure: today.text, detail: `feeds that ran for session ${session}`, href: '#data?section=market-data' }
}

export type ProbeReach = 'ok' | 'degraded' | 'fail' | 'unknown'

export function pluginsTile(probes: { name: string; reach: ProbeReach; loading: boolean }[]): DataTile {
  if (probes.some(p => p.loading)) return { name: 'Plugins', tone: 'muted', state: 'Loading', figure: '…', detail: '', href: '#data?section=plugins' }
  const bad = probes.filter(p => p.reach === 'fail')
  const degraded = probes.filter(p => p.reach === 'degraded')
  const unread = probes.filter(p => p.reach === 'unknown')
  const ok = probes.filter(p => p.reach === 'ok').map(p => `${p.name} OK`)
  const tone: Tone = bad.length ? 'fail' : degraded.length ? 'warn' : unread.length ? 'unknown' : 'ok'
  const state = bad.length ? 'Failing' : degraded.length ? 'Degraded' : unread.length ? 'Unknown' : 'OK'
  const figure = [...bad.map(p => `${p.name} down`), ...degraded.map(p => `${p.name} degraded`), ...unread.map(p => `${p.name} unknown`)][0]
    ?? `All ${probes.length} OK`
  const rest = [...bad.slice(1).map(p => `${p.name} down`), ...degraded.slice(bad.length ? 0 : 1).map(p => `${p.name} degraded`), ...ok]
  return { name: 'Plugins', tone, state, figure, detail: rest.join(' · '), href: '#data?section=plugins' }
}

const RESEARCH_TONE: Record<string, [Tone, string]> = {
  healthy: ['ok', 'OK'], due: ['info', 'Running'], caution: ['warn', 'Late'], missed: ['fail', 'Missed'], degraded: ['fail', 'Failed'],
}

export function researchTile(read: Read<OrchestrationStatusData>, now: number): DataTile {
  if (read.error != null) return unknown('Research batch', '#data?section=research', read.error)
  const d = read.data
  if (d == null) return { name: 'Research batch', tone: 'muted', state: 'Loading', figure: '…', detail: '', href: '#data?section=research' }
  const [tone, state] = RESEARCH_TONE[d.verdict] ?? ['unknown', 'Unknown']
  const figure = d.schedules_total != null ? `${d.schedules_running ?? 0}/${d.schedules_total} schedules` : d.job_name
  const last = d.last_run_ended_at ? `last run ${d.last_run_status ?? ''} ${ago(d.last_run_ended_at, now)}`.replace(/\s+/g, ' ') : 'no run recorded'
  return { name: 'Research batch', tone, state, figure, detail: last, href: '#data?section=research' }
}

export function backupTile(read: Read<ClusterPostgresBackupStatusResponse>, now: number): DataTile {
  if (read.error != null) return unknown('Backups', '#infrastructure?section=backup', read.error)
  const d = read.data
  if (d == null) return { name: 'Backups', tone: 'muted', state: 'Loading', figure: '…', detail: '', href: '#infrastructure?section=backup' }
  const tone: Tone = d.fresh ? 'ok' : 'fail'
  const figure = d.last_completed_at ? ago(d.last_completed_at, now) : 'none completed'
  return { name: 'Backups', tone, state: d.fresh ? 'OK' : 'Too old', figure, detail: `Postgres; fresh within ${d.max_age_hours} h`, href: '#infrastructure?section=backup' }
}

export function copiesTile(read: Read<DataFreshnessResponse>): DataTile {
  if (read.error != null) return unknown('Test copies', '#infrastructure', read.error)
  const d = read.data
  if (d == null) return { name: 'Test copies', tone: 'muted', state: 'Loading', figure: '…', detail: '', href: '#infrastructure' }
  const copies = d.databases.filter(db => db.verdict !== 'reference')
  const known = (db: (typeof copies)[number]) => db.verdict !== 'unknown' && typeof db.lag_vs_prod_days === 'number' && Number.isFinite(db.lag_vs_prod_days)
  if (!copies.length) return unknown('Test copies', '#infrastructure', d.detail || 'No copy of PROD is reported')
  // One copy not read is not a copy known to be fresh (review 221 A4).
  const unread = copies.filter(db => !known(db)).map(db => db.environment || db.name)
  if (unread.length) return unknown('Test copies', '#infrastructure', `${unread.join(' and ')} not read${d.detail ? ` · ${d.detail}` : ''}`)
  const worst = Math.max(...copies.map(db => db.lag_vs_prod_days as number))
  const tone: Tone = worst >= d.stale_threshold_days ? 'warn' : worst >= d.fresh_threshold_days ? 'info' : 'ok'
  const state = tone === 'warn' ? 'Stale' : tone === 'info' ? 'Aging' : 'OK'
  const names = copies.map(db => db.environment || db.name).join(' and ')
  return { name: 'Test copies', tone, state, figure: `${Math.round(worst)} days`, detail: `${names} copies of PROD, oldest`, href: '#infrastructure' }
}
