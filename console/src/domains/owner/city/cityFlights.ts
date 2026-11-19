import { placeOf, tenantOf, type CityLane } from './catalog'

/**
 * The flight log (the Owner, 2026-11-19: the system launches over and over;
 * the city should show how many times today, and replay them). Read from each
 * delivery pipeline's retained runs (GET /api/v1/delivery/pipelines/{name}/runs):
 * a PROD run is a launch (the booster alone for the platform, the booster
 * with a product's ship for a product); an STG run is a stacking in the
 * assembly building. Today is the Owner's day, in Chicago.
 */

export type Run = { name: string; pipeline: string; start_time?: string; completion_time?: string; reason?: string }
export type Flight = { run: string; pipeline: string; kind: 'prod' | 'stg'; product: { name: string; color: string } | null; start: number; end: number | null; ok: boolean | null }
export type Tally = { total: number; ok: number; failed: number; going: number }
export type Log = { flights: Flight[]; prod: Tally; booster: Tally; ships: Tally; stg: Tally; from: number }

export const FLIGHT_PIPELINES = (lanes: readonly CityLane[]) =>
  lanes.filter(l => /^bifrost-deliver-/.test(l.pipeline) || /^bifrost-build-research-dagster$/.test(l.pipeline)).map(l => l.pipeline)

/** Midnight in Chicago before `now`, as epoch ms. */
export function chicagoMidnight(now: number): number {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone: 'America/Chicago', hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' }).formatToParts(new Date(now))
  const n = (t: string) => Number(parts.find(p => p.type === t)?.value ?? 0) % 24
  return now - ((n('hour') * 60 + n('minute')) * 60 + n('second')) * 1000 - (now % 1000)
}

const tally = (fs: Flight[]): Tally => ({ total: fs.length, ok: fs.filter(f => f.ok === true).length, failed: fs.filter(f => f.ok === false).length, going: fs.filter(f => f.ok === null).length })

export function flightLog(runs: readonly Run[], lanes: readonly CityLane[], now: number): Log {
  const from = chicagoMidnight(now)
  const laneOf = new Map(lanes.map(l => [l.pipeline, l]))
  const flights: Flight[] = []
  for (const r of runs) {
    const lane = laneOf.get(r.pipeline)
    const start = r.start_time ? Date.parse(r.start_time) : NaN
    if (!lane || !Number.isFinite(start) || start < from) continue
    const at = placeOf(lane)
    const kind = at.step === 'prod' ? 'prod' : at.step === 'stg' || at.step === 'build' ? 'stg' : null
    if (!kind) continue
    const end = r.completion_time ? Date.parse(r.completion_time) : null
    const ok = end == null ? null : r.reason === 'Succeeded' || r.reason === 'Completed'
    flights.push({ run: r.name, pipeline: r.pipeline, kind, product: at.line === 'rocket' ? null : tenantOf(lane) ?? null, start, end, ok })
  }
  flights.sort((a, b) => a.start - b.start)
  const prod = flights.filter(f => f.kind === 'prod')
  return { flights, prod: tally(prod), booster: tally(prod.filter(f => !f.product)), ships: tally(prod.filter(f => f.product)), stg: tally(flights.filter(f => f.kind === 'stg')), from }
}

/** Replay: today squeezed into `length` ms; which flights are in the air at replay time t (0..length), and the day's clock then. */
export function replayAt(log: Log, now: number, t: number, length: number): { clock: number; active: Flight[] } {
  const span = Math.max(1, now - log.from)
  const clock = log.from + (t / length) * span
  // A run shorter than the replay can show stays up for at least 1.2 s of replay, so each one is seen.
  const minimum = (1200 / length) * span
  return { clock, active: log.flights.filter(f => f.start <= clock && clock <= Math.max(f.end ?? now, f.start + minimum)) }
}
