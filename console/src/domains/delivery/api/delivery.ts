import { serverFetch } from '@/api/serverContact'

import type { CompareResponse, DeliveryPipelineRunsResponse, DeliveryPipelinesResponse, DeliveryRunLogsResponse, ReleaseWindowResponse, SupplyChainResponse } from './deliveryTypes'
import { authedFetch } from '@/api/client'

/** Needs a viewer token; the other delivery reads are anonymous. */
export async function fetchReleaseWindow(): Promise<ReleaseWindowResponse> {
  const r = await authedFetch('release window', '/api/v1/delivery/release-window')
  return r.json() as Promise<ReleaseWindowResponse>
}

export async function fetchDeliveryPipelines(): Promise<DeliveryPipelinesResponse> {
  const r = await serverFetch('/api/v1/delivery/pipelines')
  if (!r.ok) throw new Error(`delivery pipelines: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryPipelinesResponse>
}

export async function fetchPipelineRuns(name: string, options?: { signal?: AbortSignal }): Promise<DeliveryPipelineRunsResponse> {
  const r = await serverFetch(`/api/v1/delivery/pipelines/${encodeURIComponent(name)}/runs`, { signal: options?.signal })
  if (!r.ok) throw new Error(`pipeline runs: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryPipelineRunsResponse>
}

export async function fetchSupplyChain(): Promise<SupplyChainResponse> {
  const r = await serverFetch('/api/v1/delivery/supply-chain')
  if (!r.ok) throw new Error(`supply chain: HTTP ${r.status}`)
  return r.json() as Promise<SupplyChainResponse>
}

/** Read-only Gitea compare — fail-soft callers treat errors as empty paths. */
export async function fetchCompare(
  repo: string,
  from: string,
  to: string,
): Promise<CompareResponse> {
  const params = new URLSearchParams({ repo, from, to })
  const r = await serverFetch(`/api/v1/delivery/compare?${params}`)
  if (!r.ok) throw new Error(`compare: HTTP ${r.status}`)
  return r.json() as Promise<CompareResponse>
}

export async function fetchPipelineRunLogs(
  runId: string,
  namespace?: string,
): Promise<DeliveryRunLogsResponse> {
  const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
  const r = await serverFetch(`/api/v1/delivery/runs/${encodeURIComponent(runId)}/logs${qs}`)
  if (!r.ok) throw new Error(`pipeline logs: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryRunLogsResponse>
}

