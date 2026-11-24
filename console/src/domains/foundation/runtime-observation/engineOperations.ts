import type { ReadEvidence } from '@bifrost/ui/city/observation'

/**
 * Engine step 6 (箭10 2026-11-24): Signals' CI workshop read through the UI engine instead of straight from the
 * platform. The catalog of pipelines comes from `/engine/v1/delivery?read=pipelines` (the platform's answer
 * unchanged) and each pipeline's retained runs from `/engine/v1/report?kind=pipeline` (an ObservationReport whose run
 * table carries every field of the platform's runs answer). The other operations reads (domain status, data
 * husbandry) stay direct: they are other steps.
 *
 * One switch per environment, all off; the direct read stays the way back. Turn an environment on only after the
 * comparison ran there as the viewer (bifrost-engine scripts/compare-report.ts and compare-listed.ts GROUP=delivery,
 * plus the CI panels compared on the page) and differed in nothing.
 */
/** Keyed by the platform's own `viewer_env` (self-health): dev, stg, prod, dev-local. */
export const ENGINE_REPORT: Readonly<Partial<Record<string, boolean>>> = {}

export const engineReportOn = (env: string | null | undefined): boolean => {
  const key = (env ?? '').trim().toLowerCase()
  return key !== '' && Object.hasOwn(ENGINE_REPORT, key) && ENGINE_REPORT[key] === true
}

type Get = (path: string) => Promise<unknown>
type Envelope<T> = { version: 1; evidence: ReadEvidence[]; data: T | null }
type Cell = string
type Report = { id: string; scope: string; evidence: ReadEvidence[]; panels: { id: string; blocks: { kind: string; head?: { label: string }[]; rows?: { cells: Cell[] }[] }[] }[] }

const PIPELINES = '/api/v1/delivery/pipelines'
const RUNS = /^\/api\/v1\/delivery\/pipelines\/([^/?]+)\/runs$/
const COLUMNS = ['Run', 'Namespace', 'Pipeline', 'Revision', 'Status', 'Reason', 'Started at', 'Completed at']
const NOT_SUPPLIED = 'Not supplied'
const REACH = /^Source reachability: ([^.]*)\. ([\s\S]*)\. Refresh deadline not supplied\.$/

/** A failed engine read is the engine's reason, never an empty answer. */
const failed = (env: Envelope<unknown>): never => {
  const e = env.evidence.find(x => x.outcome !== 'read') ?? env.evidence[0]
  throw new Error(e?.detail ?? `engine read ${e?.outcome ?? 'gave no evidence'}`)
}

/**
 * The platform's runs answer, back from the engine's pipeline report: the run table's eight columns are the runs'
 * fields ("Not supplied" is an absent field), the evidence carries the reachability, the detail and the generation
 * time, the report id the cluster. A report not in that shape is an error, not an empty list.
 */
export function runsFromReport(report: Report, pipeline: string) {
  const table = report.panels.find(p => p.id === 'runs')?.blocks.find(b => b.kind === 'table')
  const ev = report.evidence[0]
  const reach = ev?.detail ? REACH.exec(ev.detail) : null
  if (!table?.rows || table.head?.map(h => h.label).join('|') !== COLUMNS.join('|') || !reach || !ev) throw new Error('engine report not in the pipeline report shape')
  const id = JSON.parse(report.id) as unknown[]
  const opt = (v: Cell) => v === NOT_SUPPLIED ? undefined : v
  const runs = table.rows.map(({ cells: [name, namespace, pipe, revision, status, reason, start, done] }) => ({
    name, namespace, pipeline: pipe, status,
    ...(opt(revision) !== undefined ? { revision } : {}), ...(opt(reason) !== undefined ? { reason } : {}),
    ...(opt(start) !== undefined ? { start_time: start } : {}), ...(opt(done) !== undefined ? { completion_time: done } : {}) }))
  return { cluster_id: String(id[1]), namespace: report.scope.split(' / ')[1], pipeline, reachability: reach[1], detail: reach[2], runs, generated_at: ev.evaluatedAt }
}

/** The same `get` readOperations takes, answering the CI reads through the engine and the rest from `direct`. */
export function engineOperationsGet(engine: Get, direct: Get): Get {
  return async path => {
    if (path === PIPELINES) {
      const env = await engine('/engine/v1/delivery?read=pipelines') as Envelope<unknown>
      return env.data ?? failed(env)
    }
    const m = RUNS.exec(path)
    if (m) {
      const pipeline = decodeURIComponent(m[1])
      const env = await engine('/engine/v1/report?' + new URLSearchParams({ kind: 'pipeline', pipeline })) as Envelope<Report>
      return env.data ? runsFromReport(env.data, pipeline) : failed(env)
    }
    return direct(path)
  }
}
