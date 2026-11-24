import { queryOptions } from '@tanstack/react-query'
import { engineQueryKey, engineUnreachableText, readEngine } from '@/api/engine'
import type { ObservationReport } from '@bifrost/ui/city/observationReport'
import type { ReadEvidence } from '@bifrost/ui/city/observation'

/*
 * CI's run source report read from the engine backend (engine plan step 6, /engine/v1/report?kind=run; 箭10
 * 2026-11-24): the engine reads the run's timings with the viewer's token and serves the same ObservationReport the
 * Console projected itself (run.ts). The Console's own read stays as the way back.
 *   platform  the Console reads /api/v1/delivery/pipelines/{p}/timings and projects it (run.ts)
 *   engine    the engine's report
 *   compare   both, side by side with every difference listed (the check before an environment's switch flips)
 * Which one an environment uses is the table below (箭10 2026-11-24: the engine runs on STG; DEV and PROD stay on
 * the Console's read, PROD has no engine yet). A viewer can override it in this browser (localStorage), e.g. to
 * compare in STG; the override touches no one else.
 */
export type ReportSource = 'platform' | 'engine' | 'compare'
export const REPORT_SOURCE_KEY = 'bifrost.ledger.report-source'
export const REPORT_SOURCE_BY_ENV: Readonly<Record<'dev' | 'stg' | 'prod', ReportSource>> = { dev: 'platform', stg: 'engine', prod: 'platform' }

const isSource = (value: unknown): value is ReportSource => value === 'platform' || value === 'engine' || value === 'compare'

/** This viewer's override, if any (a private window or blocked storage has none). */
export function reportSourceOverride(): ReportSource | null {
  try {
    const value = globalThis.localStorage?.getItem(REPORT_SOURCE_KEY)
    return isSource(value) ? value : null
  } catch {
    return null
  }
}

/**
 * The source for an environment as the platform names it (self-health viewer_env). An environment the table does not
 * know, or one not read yet, reads the Console's own way: the switch is never turned on by a guess.
 */
export function reportSource(viewerEnv?: string | null): ReportSource {
  const override = reportSourceOverride()
  if (override) return override
  const env = (viewerEnv ?? '').trim().toLowerCase()
  return env === 'dev' || env === 'stg' || env === 'prod' ? REPORT_SOURCE_BY_ENV[env] : 'platform'
}

/** What /engine/v1/report reads: a pipeline's retained runs, one run's record, or one run's log tail. */
export type EngineReportQuery =
  | { kind: 'pipeline'; pipeline: string }
  | { kind: 'run'; pipeline: string; run: string }
  | { kind: 'logs'; run: string }

const ENGINE_REPORT = '/engine/v1/report'

/**
 * The engine's report, through the shell's one engine read (api/engine.ts, step 0). An engine that cannot be read
 * says "engine unreachable" and why; one that read and got nothing says which read failed (its own evidence). Never a
 * quiet fall back to the platform.
 */
export async function fetchEngineReport(query: EngineReportQuery, signal?: AbortSignal): Promise<ObservationReport> {
  const answer = await readEngine<ObservationReport>(ENGINE_REPORT, { query: query as Record<string, string>, signal })
  if (!answer.reached) throw new Error(engineUnreachableText(answer))
  if (answer.data == null) {
    const why = answer.evidence.map(e => `${e.source}: ${e.outcome}${e.detail ? ` (${e.detail})` : ''}`).join('; ')
    throw new Error(`Engine report: no report; ${why || 'the engine gave no evidence'}`)
  }
  return answer.data
}

export function engineReportOptions(query: EngineReportQuery, enabled = true) {
  const ready = query.kind === 'logs' ? !!query.run : query.kind === 'run' ? !!query.pipeline && !!query.run : !!query.pipeline
  return queryOptions({ queryKey: engineQueryKey(ENGINE_REPORT, query as Record<string, string>),
    queryFn: ({ signal }) => fetchEngineReport(query, signal), enabled: enabled && ready,
    staleTime: 30_000, refetchInterval: query.kind === 'logs' ? false : 30_000, retry: false })
}

export const engineRunReportOptions = (pipeline: string, run: string, enabled = true) =>
  engineReportOptions({ kind: 'run', pipeline, run }, enabled)

/**
 * Every difference between the Console's report and the engine's for one run: panels, objects and the evidence each
 * read carries. Left out on purpose, because two reads never share them: the report id and capture time, and each
 * evidence's receivedAt. The source time (evaluatedAt) is compared, so two reads of different source moments say so.
 */
export function compareRunReports(platform: ObservationReport, engine: ObservationReport): string[] {
  const out: string[] = []
  // Down to the value that differs: a list of keyed rows by key, any other list by place, an object by field.
  const diff = (path: string, a: unknown, b: unknown): void => {
    if (Array.isArray(a) && Array.isArray(b)) {
      const keyed = [...a, ...b].every(item => item && typeof item === 'object' && typeof (item as { key?: unknown }).key === 'string')
      if (keyed) {
        const byKey = (list: unknown[]) => new Map(list.map(item => [(item as { key: string }).key, item]))
        const left = byKey(a), right = byKey(b)
        for (const key of new Set([...left.keys(), ...right.keys()])) diff(`${path}[${key}]`, left.get(key), right.get(key))
        if (JSON.stringify([...left.keys()]) !== JSON.stringify([...right.keys()]) && left.size === right.size) out.push(`${path}: rows in another order`)
        return
      }
      for (let i = 0; i < Math.max(a.length, b.length); i++) diff(`${path}[${i}]`, a[i], b[i])
      return
    }
    if (a && b && typeof a === 'object' && typeof b === 'object' && !Array.isArray(a) && !Array.isArray(b)) {
      for (const field of new Set([...Object.keys(a), ...Object.keys(b)])) diff(`${path}.${field}`, (a as Record<string, unknown>)[field], (b as Record<string, unknown>)[field])
      return
    }
    const left = JSON.stringify(a), right = JSON.stringify(b)
    if (left !== right) out.push(`${path}: console ${clip(left)} · engine ${clip(right)}`)
  }
  const evidence = (list: readonly ReadEvidence[]) => list.map(({ receivedAt: _receivedAt, ...rest }) => rest)
  diff('version', platform.version, engine.version)
  diff('scope', platform.scope, engine.scope)
  diff('evidence', evidence(platform.evidence), evidence(engine.evidence))
  diff('panels', platform.panels, engine.panels)
  for (const key of new Set([...Object.keys(platform.objects), ...Object.keys(engine.objects)])) {
    const a = platform.objects[key], b = engine.objects[key]
    diff(`objects[${key}]`, a && { ...a, evidence: evidence(a.evidence) }, b && { ...b, evidence: evidence(b.evidence) })
  }
  return out
}

const clip = (text: string | undefined) => (text === undefined ? 'absent' : text.length > 160 ? `${text.slice(0, 160)}…` : text)
