import { queryOptions } from '@tanstack/react-query'
import { authedFetch } from '@/api/client'
import type { ObservationReport } from '@bifrost/ui/city/observationReport'
import type { ReadEvidence } from '@bifrost/ui/city/observation'

/*
 * CI's run source report read from the engine backend (engine plan step 6, /engine/v1/report?kind=run; 箭10
 * 2026-11-24): the engine reads the run's timings with the viewer's token and serves the same ObservationReport the
 * Console projected itself (run.ts). The Console's own read stays as the way back. Which one a viewer sees is a
 * per-viewer switch, off by default:
 *   platform (default)  the Console reads /api/v1/delivery/pipelines/{p}/timings and projects it (run.ts)
 *   engine              the engine's report
 *   compare             both, side by side with every difference listed (the check before the switch flips)
 * It lives in this browser only (localStorage), so turning it on in STG to compare touches no one else.
 */
export type ReportSource = 'platform' | 'engine' | 'compare'
export const REPORT_SOURCE_KEY = 'bifrost.ledger.report-source'

export function reportSource(): ReportSource {
  try {
    const value = globalThis.localStorage?.getItem(REPORT_SOURCE_KEY)
    return value === 'engine' || value === 'compare' ? value : 'platform'
  } catch {
    return 'platform'
  }
}

/** The engine's answer (API v1): who read what, and the report or null when a read failed. */
export type EngineEnvelope<T> = { version: 1; model: string; capturedAt: string; evidence: ReadEvidence[]; data: T | null }

export const engineRunReportPath = (pipeline: string, run: string) =>
  `/engine/v1/report?${new URLSearchParams({ kind: 'run', pipeline, run })}`

/** The engine's run report. A failed upstream read comes back as an error naming the engine's own evidence. */
export async function fetchEngineRunReport(pipeline: string, run: string, signal?: AbortSignal): Promise<{ report: ObservationReport; envelope: EngineEnvelope<ObservationReport> }> {
  const response = await authedFetch('Engine run report', engineRunReportPath(pipeline, run), { signal })
  const body = await response.json() as EngineEnvelope<ObservationReport>
  if (body?.version !== 1 || body.model !== 'report' || !Array.isArray(body.evidence)) throw new Error('Engine run report: not an engine v1 report answer')
  if (body.data == null) {
    const why = body.evidence.map(e => `${e.source}: ${e.outcome}${e.detail ? ` (${e.detail})` : ''}`).join('; ')
    throw new Error(`Engine run report: no report; ${why || 'the engine gave no evidence'}`)
  }
  return { report: body.data, envelope: body }
}

export function engineRunReportOptions(pipeline: string, run: string, enabled = true) {
  return queryOptions({ queryKey: ['engine', 'report', 'run', pipeline, run] as const,
    queryFn: ({ signal }) => fetchEngineRunReport(pipeline, run, signal), enabled: enabled && !!pipeline && !!run,
    staleTime: 30_000, refetchInterval: 30_000, retry: false })
}

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
