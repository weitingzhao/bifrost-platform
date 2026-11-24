import type { ReadEvidence } from '@bifrost/ui/city/observation'
import type { CapturedReads } from '../runtime-observation/read'

/**
 * Engine step 2 (箭10 2026-11-24): the hardware read through the UI engine (`GET /engine/v1/hardware`) instead of
 * straight from the platform. One switch per environment, all off: the direct read stays the way back.
 * Turn an environment on only after `bifrost-engine/scripts/compare-hardware.ts` ran there as the viewer and differed
 * in nothing; turning it off again is this table, nothing else.
 */
/** Keyed by the platform's own `viewer_env` (self-health): dev, stg, prod, dev-local. */
export const ENGINE_HARDWARE: Readonly<Partial<Record<string, boolean>>> = {}

export const ENGINE_HARDWARE_PATH = '/engine/v1/hardware'

/** Whether this environment reads the hardware through the engine. Unknown (still loading) is the direct read. */
export const engineHardwareOn = (env: string | null | undefined): boolean => {
  const key = (env ?? '').trim().toLowerCase()
  return key !== '' && Object.hasOwn(ENGINE_HARDWARE, key) && ENGINE_HARDWARE[key] === true
}

type EnginePoint = { labels?: Record<string, string>; value: number; timestamp?: number }
/** The fields of the engine's v1 hardware envelope this adapter reads (bifrost-engine src/hardware.ts). */
export type EngineHardware = {
  version: 1
  evidence: ReadEvidence[]
  data: {
    topology: unknown[] | null
    prom: Record<string, EnginePoint[] | null>
    alerts: unknown[] | null
    queries: Record<string, string>
  } | null
}

const TOPOLOGY = '/api/v1/topology?env=prod'
const ALERTS = '/api/v1/telemetry/alerts'
const queryPath = (q: string) => '/api/v1/telemetry/promql?q=' + encodeURIComponent(q)

/**
 * The engine's answer as the captures the direct read makes, path by path, so the same snapshotOf turns either into
 * the same ObservationSnapshot: a read the engine says failed is a failed capture with the engine's reason; a read
 * the engine did not make is no capture at all (snapshotOf says "Missing capture"). Nothing is judged here.
 */
export function capturedFromEngine(answer: EngineHardware): CapturedReads {
  const data = answer.data
  const pathOfKey = new Map(Object.entries(data?.queries ?? {}).map(([key, q]) => [queryPath(q), key]))
  const bodyOf = (source: string, evaluatedAt?: string): unknown => {
    if (source === TOPOLOGY) return data?.topology ? { nodes: data.topology, ...(evaluatedAt ? { generated_at: evaluatedAt } : {}) } : undefined
    if (source === ALERTS) return data?.alerts ? { alerts: data.alerts, ...(evaluatedAt ? { generated_at: evaluatedAt } : {}) } : undefined
    const key = pathOfKey.get(source)
    const points = key === undefined ? undefined : data?.prom[key]
    return points ? { points } : undefined
  }
  const reads: CapturedReads = {}
  for (const e of answer.evidence) {
    const body = e.outcome === 'error' ? undefined : bodyOf(e.source, e.evaluatedAt)
    reads[e.source] = body !== undefined ? { body, receivedAt: e.receivedAt } : { error: e.detail ?? `engine read ${e.outcome}`, receivedAt: e.receivedAt }
  }
  return reads
}
