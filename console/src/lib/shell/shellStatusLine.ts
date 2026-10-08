import type { SelfHealthProbe, SelfHealthProbeStatus, SelfHealthResponse } from '@/api/matrixTypes'
import { normalizeViewerEnv } from '@/lib/control-room/fleetSnapshot'

/** Shell status reads this path and no environment query. */
export const SHELL_STATUS_PATH = '/api/v1/self-health'

const RANK: Record<SelfHealthProbeStatus, number> = {
  ok: 0,
  unknown: 1,
  degraded: 2,
  fail: 3,
}

/** Probes for the console's own seat. Other environments are not part of the sentence. */
export function probesForViewer(health: SelfHealthResponse): SelfHealthProbe[] {
  const env = normalizeViewerEnv(health.viewer_env)
  const matched = health.probes.filter(probe => probe.env.toLowerCase() === env)
  if (matched.length > 0) return matched
  return health.probes.filter(probe => probe.env === '')
}

function worstStatus(probes: readonly SelfHealthProbe[]): SelfHealthProbeStatus {
  let worst: SelfHealthProbeStatus = 'ok'
  for (const probe of probes) {
    if (RANK[probe.status] > RANK[worst]) worst = probe.status
  }
  return worst
}

function sentenceFor(status: SelfHealthProbeStatus, detail: string): string {
  if (status === 'ok') return 'All clear'
  if (status === 'fail') return detail === '' ? 'Failing' : `Failing — ${detail}`
  if (status === 'degraded') return detail === '' ? 'Degraded' : `Degraded — ${detail}`
  return detail === '' ? 'Unknown' : `Unknown — ${detail}`
}

export function shellStatusSentence(input: {
  health: SelfHealthResponse | undefined
  loading: boolean
  error: string | null
}): string {
  if (input.loading) return 'Checking health…'
  if (input.error != null && input.error !== '') return input.error
  const health = input.health
  if (health == null) return 'Checking health…'
  const probes = probesForViewer(health)
  if (probes.length === 0) {
    return sentenceFor(health.overall, '')
  }
  const status = worstStatus(probes)
  if (status === 'ok') return 'All clear'
  const cause = probes.find(probe => probe.status === status)
  const detail = cause?.detail.trim() || cause?.id || ''
  return sentenceFor(status, detail)
}

export function shellViewerHealthy(health: SelfHealthResponse | undefined): boolean | undefined {
  if (health == null) return undefined
  const probes = probesForViewer(health)
  if (probes.length === 0) return health.overall === 'ok'
  return worstStatus(probes) === 'ok'
}
