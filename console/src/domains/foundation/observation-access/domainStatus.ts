import { queryOptions, useQuery } from '@tanstack/react-query'
import type { ReadEvidence } from '@bifrost/ui/city/observation'
import { engineQueryKey, engineUnreachableText, readEngine } from '@/api/engine'
import { fetchSelfHealth } from '@/api/core'
import { fetchDomainStatus, type DomainStatusResponse } from '../catalog/api/domainStatus'

/*
 * Engine switch 1b + 1d (UI / Engine 2026-11-24: one row, the Console line's): Console Status, the shell's verdict
 * strip and Signals share this one reader, so it switches once. On in an environment, the four lights come from
 * GET /engine/v1/domains (bifrost-engine domains.ts passes /api/v1/catalog/status through unchanged) and the evidence
 * is the engine's. Off, the direct read below, unchanged. The engine unreachable is said so, never a quiet fall back.
 * Compared on STG 2026-11-24 06:54Z (engine cae00ea): domains same. Turning it off again is this table.
 */
/** Keyed by the platform's own `viewer_env` (self-health): dev, stg, prod, dev-local. PROD has no engine yet. */
export const ENGINE_DOMAINS: Readonly<Partial<Record<string, boolean>>> = { stg: true }
export const ENGINE_DOMAINS_PATH = '/engine/v1/domains'

/** Whether this environment reads the domains through the engine. Unknown (self-health still loading) is the direct read. */
export const engineDomainsOn = (env: string | null | undefined): boolean => {
  const key = (env ?? '').trim().toLowerCase()
  return key !== '' && Object.hasOwn(ENGINE_DOMAINS, key) && ENGINE_DOMAINS[key] === true
}

/** Same raw cache shape as Console Status; no CI, data, mailbox or client health verdict. */
export function domainStatusObservationOptions() {
  return queryOptions({
    queryKey: ['status', 'domain-status'] as const,
    queryFn: async ({ signal }) => {
      const data = await fetchDomainStatus({ signal })
      signal.throwIfAborted()
      return data
    },
    staleTime: 60_000,
    refetchInterval: 60_000,
    retry: false,
  })
}

/** The engine's answer: the platform's response unchanged, with the engine's evidence of that read. */
export type EngineDomainStatus = { data: DomainStatusResponse; evidence: ReadEvidence }

/** Through the engine: another key (['engine', …]), so one cache never mixes the two sources and an epoch change drops it. */
export function engineDomainStatusOptions() {
  return queryOptions({
    queryKey: engineQueryKey(ENGINE_DOMAINS_PATH),
    queryFn: async ({ signal }): Promise<EngineDomainStatus> => {
      const a = await readEngine<DomainStatusResponse>(ENGINE_DOMAINS_PATH, { signal })
      signal.throwIfAborted()
      if (!a.reached) throw new Error(engineUnreachableText(a))
      const evidence = a.evidence[0]
      if (a.data == null || !Array.isArray(a.data.domains)) {
        throw new Error('domain status (engine): ' + (evidence?.detail ?? 'incomplete answer; the lights are not known'))
      }
      return { data: a.data, evidence }
    },
    staleTime: 60_000,
    refetchInterval: 60_000,
    retry: false,
  })
}

// Existing Signals display window, not a domain health rule. Old sources never become fresh on retry.
const expiryOf = (evaluatedAt: string | undefined, updatedAt: number): string | undefined => {
  const evaluated = evaluatedAt ? Date.parse(evaluatedAt) : NaN
  return Number.isFinite(evaluated) ? new Date(Math.min(evaluated, updatedAt) + 90_000).toISOString() : undefined
}

/** Read evidence is separate from the unmodified authoritative response and its four lights. */
export function useDomainStatusObservation() {
  // The shell's self-health query (same key, so one read): only its viewer_env is needed here.
  const health = useQuery({ queryKey: ['shell', 'self-health'], queryFn: () => fetchSelfHealth(), staleTime: 10_000 })
  const viaEngine = engineDomainsOn(health.data?.viewer_env)
  const direct = useQuery({ ...domainStatusObservationOptions(), enabled: !viaEngine })
  const engine = useQuery({ ...engineDomainStatusOptions(), enabled: viaEngine })

  if (viaEngine) {
    const received = engine.error ? engine.errorUpdatedAt : engine.dataUpdatedAt
    const held = engine.data
    const evidence: ReadEvidence | undefined = !received ? undefined : engine.error
      ? { source: held?.evidence.source ?? ENGINE_DOMAINS_PATH, outcome: 'error', receivedAt: new Date(received).toISOString(), detail: engine.error.message }
      : held ? { ...held.evidence, expiresAt: held.evidence.expiresAt ?? expiryOf(held.data.at, engine.dataUpdatedAt) } : undefined
    return { ...engine, data: held?.data, evidence }
  }

  const received = direct.error ? direct.errorUpdatedAt : direct.dataUpdatedAt
  const evaluatedAt = direct.data?.at
  const evidence: ReadEvidence | undefined = received ? {
    source: '/api/v1/catalog/status',
    outcome: direct.error ? 'error' : 'read',
    receivedAt: new Date(received).toISOString(),
    evaluatedAt,
    expiresAt: expiryOf(evaluatedAt, direct.dataUpdatedAt),
    detail: direct.error?.message,
  } : undefined
  return { ...direct, evidence }
}
