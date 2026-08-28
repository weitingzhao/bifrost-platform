import type { ActuationResponse } from './matrixTypes'
import type { DeliveryPipelinePreflightResponse, DeliveryPipelineRunsResponse, DeliveryPipelinesResponse, DeliveryRunLogsResponse, DeliveryStartRunResponse, PipelineRunStepsResponse, RefPreflightResponse, RevisionsResponse, SupplyChainActuationResponse, SupplyChainResponse } from './deliveryTypes'
import { authedFetch } from './client'

export async function fetchDeliveryPipelines(): Promise<DeliveryPipelinesResponse> {
  const r = await fetch('/api/v1/delivery/pipelines')
  if (!r.ok) throw new Error(`delivery pipelines: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryPipelinesResponse>
}

export async function fetchPipelinePreflight(
  pipelineName: string,
): Promise<DeliveryPipelinePreflightResponse> {
  const r = await fetch(`/api/v1/delivery/pipelines/${encodeURIComponent(pipelineName)}/preflight`)
  if (!r.ok) throw new Error(`delivery preflight: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryPipelinePreflightResponse>
}

export async function fetchPipelineRuns(name: string): Promise<DeliveryPipelineRunsResponse> {
  const r = await fetch(`/api/v1/delivery/pipelines/${encodeURIComponent(name)}/runs`)
  if (!r.ok) throw new Error(`pipeline runs: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryPipelineRunsResponse>
}

export async function fetchSupplyChain(): Promise<SupplyChainResponse> {
  const r = await fetch('/api/v1/delivery/supply-chain')
  if (!r.ok) throw new Error(`supply chain: HTTP ${r.status}`)
  return r.json() as Promise<SupplyChainResponse>
}

export async function fetchRevisions(repos?: string[]): Promise<RevisionsResponse> {
  const params = repos?.length ? `?repos=${repos.join(',')}` : ''
  const r = await fetch(`/api/v1/delivery/revisions${params}`)
  if (!r.ok) throw new Error(`revisions: HTTP ${r.status}`)
  return r.json() as Promise<RevisionsResponse>
}

export async function fetchRefPreflight(
  pipeline: string,
  revision: string,
): Promise<RefPreflightResponse> {
  const params = new URLSearchParams({ revision })
  const r = await fetch(`/api/v1/delivery/pipelines/${pipeline}/ref-preflight?${params}`)
  if (!r.ok) throw new Error(`ref preflight: HTTP ${r.status}`)
  return r.json() as Promise<RefPreflightResponse>
}

export async function triggerMirrorSync(): Promise<SupplyChainActuationResponse> {
  const r = await authedFetch('mirror sync', '/api/v1/delivery/supply-chain/mirror-sync', {
    method: 'POST',
  })
  return r.json() as Promise<SupplyChainActuationResponse>
}

export async function refreshDockerfileConfigMaps(
  revision = 'main',
): Promise<SupplyChainActuationResponse> {
  const r = await authedFetch(
    'refresh dockerfile configmaps',
    '/api/v1/delivery/supply-chain/dockerfile-configmaps/refresh',
    { method: 'POST', body: JSON.stringify({ revision }) },
  )
  return r.json() as Promise<SupplyChainActuationResponse>
}

export async function startPipelineRun(
  name: string,
  revision?: string,
  agentSessionId?: string,
  /** Image tag to build. Only bifrost-deliver-research consumes it today. */
  tag?: string,
): Promise<DeliveryStartRunResponse> {
  const payload: { revision?: string; tag?: string } = {}
  if (revision != null && revision.trim() !== '') payload.revision = revision.trim()
  if (tag != null && tag.trim() !== '') payload.tag = tag.trim()
  const body = Object.keys(payload).length > 0 ? JSON.stringify(payload) : undefined
  const headers: Record<string, string> = {}
  const session = agentSessionId?.trim()
  if (session) headers['X-Agent-Session-ID'] = session
  const r = await authedFetch(
    'pipeline run',
    `/api/v1/delivery/pipelines/${encodeURIComponent(name)}/runs`,
    { method: 'POST', body, headers },
  )
  return r.json() as Promise<DeliveryStartRunResponse>
}

export async function fetchPipelineRunLogs(
  runId: string,
  namespace?: string,
): Promise<DeliveryRunLogsResponse> {
  const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
  const r = await fetch(`/api/v1/delivery/runs/${encodeURIComponent(runId)}/logs${qs}`)
  if (!r.ok) throw new Error(`pipeline logs: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryRunLogsResponse>
}

export async function fetchPipelineRunSteps(
  runId: string,
  namespace?: string,
): Promise<PipelineRunStepsResponse> {
  const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
  const r = await fetch(`/api/v1/delivery/runs/${encodeURIComponent(runId)}/steps${qs}`)
  if (!r.ok) throw new Error(`pipeline steps: HTTP ${r.status}`)
  return r.json() as Promise<PipelineRunStepsResponse>
}

export async function deletePipelineRun(
  runId: string,
  namespace?: string,
): Promise<ActuationResponse> {
  const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
  const r = await authedFetch(
    'delete pipeline run',
    `/api/v1/delivery/runs/${encodeURIComponent(runId)}${qs}`,
    { method: 'DELETE' },
  )
  return r.json() as Promise<ActuationResponse>
}

