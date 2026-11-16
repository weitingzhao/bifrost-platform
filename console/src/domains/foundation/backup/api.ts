import { serverFetch } from '@/api/serverContact'
import type { ClusterPostgresBackupStatusResponse } from './types'

export async function fetchClusterPostgresBackupStatus(): Promise<ClusterPostgresBackupStatusResponse> {
  const r = await serverFetch('/api/v1/cluster/postgres/backup-status')
  if (!r.ok) throw new Error(`cluster postgres backup-status: HTTP ${r.status}`)
  return r.json() as Promise<ClusterPostgresBackupStatusResponse>
}

