import type { ActuationResponse, PodLogsResponse, RolloutRestartRequest, ScaleRequest } from './matrixTypes'
import type { ClusterSyncResponse, DrainNodeRequest } from './clusterTypes'
import { authedFetch, parseError } from './client'

export async function wakeComputeNode(nodeName: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'wake compute node',
    `/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/wake`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function powerOffComputeNode(nodeName: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'power off compute node',
    `/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/poweroff`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function cordonNode(nodeName: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'cordon node',
    `/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/cordon`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function uncordonNode(nodeName: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'uncordon node',
    `/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/uncordon`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function drainNode(
  nodeName: string,
  body: DrainNodeRequest = { force: true, grace_period_seconds: 60 },
): Promise<ActuationResponse> {
  const r = await authedFetch(
    'drain node',
    `/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/drain`,
    { method: 'POST', body: JSON.stringify(body) },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function joinClusterNode(profile: string): Promise<ActuationResponse> {
  const r = await authedFetch('join cluster node', '/api/v1/cluster/nodes/join', {
    method: 'POST',
    body: JSON.stringify({ profile }),
  })
  return r.json() as Promise<ActuationResponse>
}

export async function syncClusterKubeconfig(): Promise<ClusterSyncResponse> {
  const r = await fetch('/api/v1/cluster/sync-kubeconfig', { method: 'POST' })
  if (!r.ok) throw new Error(`sync kubeconfig: HTTP ${r.status}`)
  return r.json() as Promise<ClusterSyncResponse>
}

export async function ensureBifrostNamespaces(): Promise<ActuationResponse> {
  const r = await authedFetch('ensure bifrost namespaces', '/api/v1/cluster/namespaces/ensure-bifrost', {
    method: 'POST',
  })
  return r.json() as Promise<ActuationResponse>
}

export async function ensureMetricsServer(): Promise<ActuationResponse> {
  const r = await authedFetch('ensure metrics-server', '/api/v1/cluster/addons/metrics-server/ensure', {
    method: 'POST',
  })
  return r.json() as Promise<ActuationResponse>
}

export async function ensureKubePrometheusStack(): Promise<ActuationResponse> {
  const r = await authedFetch(
    'ensure kube-prometheus-stack',
    '/api/v1/cluster/addons/kube-prometheus-stack/ensure',
    {
      method: 'POST',
    },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function rolloutRestartDeployment(
  body: RolloutRestartRequest,
): Promise<ActuationResponse> {
  const r = await authedFetch('rollout restart', '/api/v1/cluster/workloads/rollout-restart', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<ActuationResponse>
}

export async function scaleDeployment(body: ScaleRequest): Promise<ActuationResponse> {
  const r = await authedFetch('scale deployment', '/api/v1/cluster/workloads/scale', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<ActuationResponse>
}

export async function deletePod(namespace: string, name: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'delete pod',
    `/api/v1/cluster/workloads/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    { method: 'DELETE' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function fetchPodLogs(
  namespace: string,
  name: string,
  tailLines = 200,
): Promise<PodLogsResponse> {
  const params = new URLSearchParams({ tailLines: String(tailLines) })
  const r = await fetch(
    `/api/v1/cluster/workloads/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/logs?${params}`,
  )
  if (!r.ok) throw await parseError('pod logs', r)
  return r.json() as Promise<PodLogsResponse>
}

