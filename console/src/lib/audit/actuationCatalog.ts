import type { AuditRecord } from '@/api/types'

/** Filter chips for AuditRecordsPanel — same taxonomy as WRITE_PATHS actors. */
export type ActuationCategory = 'all' | 'migrate-wave' | 'drift' | 'gitops' | 'cluster' | 'other'

export const ACTUATION_CATEGORY_OPTIONS: { id: ActuationCategory; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'migrate-wave', label: 'Migrate waves' },
  { id: 'drift', label: 'Drift / Agent' },
  { id: 'gitops', label: 'GitOps' },
  { id: 'cluster', label: 'Cluster' },
  { id: 'other', label: 'Other' },
]

export function actuationCategory(action: string): ActuationCategory {
  const a = action.toLowerCase()
  if (a.startsWith('migratewave.')) return 'migrate-wave'
  if (
    a.includes('drift') ||
    a.includes('remediation') ||
    a.includes('agent.') ||
    a.includes('nightly')
  ) {
    return 'drift'
  }
  if (
    a.includes('argocd') ||
    a.includes('gitops') ||
    a.includes('tekton') ||
    a.includes('delivery') ||
    a.includes('promote')
  ) {
    return 'gitops'
  }
  if (
    a.includes('cluster') ||
    a.includes('workload') ||
    a.includes('kubectl') ||
    a.includes('k8s')
  ) {
    return 'cluster'
  }
  return 'other'
}

export function filterAuditByCategory(
  records: AuditRecord[],
  category: ActuationCategory,
): AuditRecord[] {
  if (category === 'all') return records
  return records.filter(r => actuationCategory(r.action) === category)
}

/** Human label for migrate wave audit rows (target = streamId/waveId). */
export function formatMigrateWaveAuditLabel(record: AuditRecord): string {
  const [streamId, waveId] = record.target.split('/')
  const verb =
    record.action === 'migratewave.deliver'
      ? 'Delivered'
      : record.action === 'migratewave.signoff'
        ? 'Signed off'
        : record.action.replace('migratewave.', '')
  const wave = waveId ?? record.target
  return `${verb} · ${streamId ?? 'migrate'} / ${wave}`
}

export function isMigrateWaveAudit(record: AuditRecord): boolean {
  return record.action.startsWith('migratewave.')
}

export function migrateWaveAuditForStream(
  records: AuditRecord[],
  streamId: string,
  limit = 5,
): AuditRecord[] {
  return records
    .filter(r => isMigrateWaveAudit(r) && r.target.startsWith(`${streamId}/`))
    .slice(0, limit)
}
