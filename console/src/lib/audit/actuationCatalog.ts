import type { AuditRecord } from '@/api/auditTypes'
import type { DenseTagVariant } from '@bifrost/ui'

/** Filter chips for AuditRecordsPanel — same taxonomy as WRITE_PATHS actors. */
export type ActuationCategory = 'all' | 'migrate-wave' | 'drift' | 'gitops' | 'cluster' | 'other'

/** Who effectively performed the write — distinct from Actor name (often the operator who started a job). */
export type ActuationOrigin = 'human' | 'agent' | 'system'

export const ACTUATION_CATEGORY_OPTIONS: { id: ActuationCategory; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'migrate-wave', label: 'Migrate waves' },
  { id: 'drift', label: 'Drift / Agent' },
  { id: 'gitops', label: 'GitOps' },
  { id: 'cluster', label: 'Cluster' },
  { id: 'other', label: 'Other' },
]

/** Shared DenseTag colors — filter chips and Category column must match. */
export const ACTUATION_CATEGORY_VARIANT: Record<
  Exclude<ActuationCategory, 'all'>,
  DenseTagVariant
> = {
  'migrate-wave': 'category',
  drift: 'warning',
  gitops: 'info',
  cluster: 'success',
  other: 'neutral',
}

const CATEGORY_ROW_LABEL: Record<Exclude<ActuationCategory, 'all'>, string> = {
  'migrate-wave': 'Migrate',
  drift: 'Drift/Agent',
  gitops: 'GitOps',
  cluster: 'Cluster',
  other: 'Other',
}

const SYSTEM_ACTORS = new Set([
  'ops-agent',
  'agent-deploy',
  'queue-sweep',
  'platform-api',
])

export function actuationCategory(action: string): Exclude<ActuationCategory, 'all'> {
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

export function actuationCategoryLabel(category: Exclude<ActuationCategory, 'all'>): string {
  return CATEGORY_ROW_LABEL[category]
}

/**
 * Infer write origin from action + actor.
 * Actor alone is not enough: remediation.done often keeps the operator name who started the job.
 */
export function actuationOrigin(record: AuditRecord): ActuationOrigin {
  const actor = (record.actor ?? '').toLowerCase()
  const action = (record.action ?? '').toLowerCase()
  if (
    SYSTEM_ACTORS.has(actor) ||
    actor.startsWith('ops-agent') ||
    action.startsWith('ops-agent.')
  ) {
    return 'system'
  }
  if (
    action === 'remediation.done' ||
    action === 'remediation.failed' ||
    action.startsWith('agent.') ||
    action.includes('nightly')
  ) {
    return 'agent'
  }
  return 'human'
}

export const ACTUATION_ORIGIN_OPTIONS: { id: ActuationOrigin | 'all'; label: string }[] = [
  { id: 'all', label: 'All origins' },
  { id: 'human', label: 'Human' },
  { id: 'agent', label: 'Agent' },
  { id: 'system', label: 'System' },
]

/**
 * Origin tags use a filled chip (not outline) and Agent uses violet —
 * so they do not collide with Category outline colors (Drift/Agent=amber, GitOps=sky).
 */
export function actuationOriginTagClass(origin: ActuationOrigin): string {
  switch (origin) {
    case 'human':
      return 'border-sky-500/50 bg-sky-500/18 text-sky-800 dark:text-sky-200 font-semibold tracking-wide'
    case 'agent':
      return 'border-violet-500/50 bg-violet-500/18 text-violet-800 dark:text-violet-200 font-semibold tracking-wide'
    case 'system':
      return 'border-border bg-muted/70 text-muted-foreground font-semibold tracking-wide'
  }
}

export function actuationOriginLabel(origin: ActuationOrigin): string {
  if (origin === 'human') return 'Human'
  if (origin === 'agent') return 'Agent'
  return 'System'
}

export function filterAuditByCategory(
  records: AuditRecord[],
  category: ActuationCategory,
): AuditRecord[] {
  if (category === 'all') return records
  return records.filter(r => actuationCategory(r.action) === category)
}

export function filterAuditByOrigin(
  records: AuditRecord[],
  origin: ActuationOrigin | 'all',
): AuditRecord[] {
  if (origin === 'all') return records
  return records.filter(r => actuationOrigin(r) === origin)
}

export function filterAuditRecords(
  records: AuditRecord[],
  category: ActuationCategory,
  origin: ActuationOrigin | 'all',
): AuditRecord[] {
  return filterAuditByOrigin(filterAuditByCategory(records, category), origin)
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
