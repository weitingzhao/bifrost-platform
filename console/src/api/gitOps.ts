import { serverFetch } from '@/api/serverContact'

import type { GitOpsAppsResponse } from './deliveryTypes'
import './client'

export async function fetchGitOpsApps(): Promise<GitOpsAppsResponse> {
  const r = await serverFetch('/api/v1/gitops/apps')
  if (!r.ok) throw new Error(`gitops apps: HTTP ${r.status}`)
  return r.json() as Promise<GitOpsAppsResponse>
}

