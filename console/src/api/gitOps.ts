import type { ActuationResponse } from './matrixTypes'
import type { GitOpsAppsResponse } from './deliveryTypes'
import { authedFetch } from './client'

export async function fetchGitOpsApps(): Promise<GitOpsAppsResponse> {
  const r = await fetch('/api/v1/gitops/apps')
  if (!r.ok) throw new Error(`gitops apps: HTTP ${r.status}`)
  return r.json() as Promise<GitOpsAppsResponse>
}

export async function syncGitOpsApp(name: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'gitops sync',
    `/api/v1/gitops/apps/${encodeURIComponent(name)}/sync`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function rollbackGitOpsApp(
  name: string,
  revision?: string,
): Promise<ActuationResponse> {
  const body =
    revision != null && revision !== '' ? JSON.stringify({ revision }) : JSON.stringify({})
  const r = await authedFetch(
    'gitops rollback',
    `/api/v1/gitops/apps/${encodeURIComponent(name)}/rollback`,
    { method: 'POST', body },
  )
  return r.json() as Promise<ActuationResponse>
}

