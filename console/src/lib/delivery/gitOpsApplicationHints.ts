import type { AuditRecord, GitOpsApplicationView } from '@/api/types'

export type GitOpsHintLevel = 'recommend' | 'optional' | 'noop' | 'blocked' | 'unavailable'

export interface GitOpsActionHint {
  level: GitOpsHintLevel
  message: string
  shortLabel: string
}

export interface GitOpsApplicationHints {
  sync: GitOpsActionHint
  rollback: GitOpsActionHint
}

function syncStatus(app: GitOpsApplicationView): string {
  return app.sync_status.toLowerCase()
}

function healthStatus(app: GitOpsApplicationView): string {
  return app.health_status.toLowerCase()
}

export function gitOpsApplicationHints(app: GitOpsApplicationView): GitOpsApplicationHints {
  const sync = syncStatus(app)
  const health = healthStatus(app)
  const historyCount = app.history_count ?? 0

  let syncHint: GitOpsActionHint
  if (sync === 'outofsync') {
    syncHint = {
      level: 'recommend',
      shortLabel: 'Sync needed',
      message:
        'Git and cluster differ (OutOfSync). Sync applies the latest Git revision from the source repo to the destination namespace.',
    }
  } else if (sync === 'synced' && health === 'healthy') {
    syncHint = {
      level: 'noop',
      shortLabel: 'Likely no-op',
      message:
        'Already Synced + Healthy. Sync usually changes nothing unless Git moved since the last refresh. Automated sync may already be handling drift.',
    }
  } else if (sync === 'synced' && (health === 'degraded' || health === 'progressing')) {
    syncHint = {
      level: 'optional',
      shortLabel: 'Sync optional',
      message:
        'Manifests match Git but workloads are not fully healthy. Sync alone may not fix runtime issues — check pods in Cluster or pipeline/image changes.',
    }
  } else if (sync === 'unknown' && app.primary_condition != null && app.primary_condition !== '') {
    syncHint = {
      level: 'recommend',
      shortLabel: 'Comparison error',
      message: `${app.primary_condition} Fix Argo CD RBAC or repo access before Sync will succeed.`,
    }
  } else if (sync === 'unknown') {
    syncHint = {
      level: 'unavailable',
      shortLabel: 'Status unknown',
      message: 'Argo CD sync status is unknown — open the Application drawer for conditions or verify Argo CD server health.',
    }
  } else {
    syncHint = {
      level: 'optional',
      shortLabel: 'Sync optional',
      message: 'Manual Sync re-applies the current Git target revision. Use after Git updates or when automated sync is disabled.',
    }
  }

  let rollbackHint: GitOpsActionHint
  if (historyCount < 2) {
    rollbackHint = {
      level: 'blocked',
      shortLabel: 'No prior revision',
      message: `Rollback needs at least 2 deployment history entries (currently ${historyCount}). First deploy or fresh Application cannot roll back.`,
    }
  } else if (health === 'degraded' || health === 'missing' || health === 'suspended') {
    rollbackHint = {
      level: 'recommend',
      shortLabel: 'Rollback candidate',
      message:
        'Workloads unhealthy — Rollback re-syncs to the previous successfully deployed Git revision. Confirm impact on prod before using.',
    }
  } else {
    rollbackHint = {
      level: 'optional',
      shortLabel: 'Rollback available',
      message: `Previous revision available (${historyCount} history entries). Use only when the current deployment is bad — not for routine restarts.`,
    }
  }

  return { sync: syncHint, rollback: rollbackHint }
}

export function hintTagVariant(level: GitOpsHintLevel): 'success' | 'warning' | 'neutral' | 'danger' {
  switch (level) {
    case 'recommend':
      return 'warning'
    case 'blocked':
      return 'danger'
    case 'noop':
      return 'neutral'
    default:
      return 'neutral'
  }
}

export function auditTargetForApp(app: GitOpsApplicationView): string {
  return `Application/${app.namespace}/${app.name}`
}

export function filterAuditForApp(records: AuditRecord[], app: GitOpsApplicationView): AuditRecord[] {
  const target = auditTargetForApp(app)
  return records.filter(
    r =>
      r.target === target ||
      (r.action.startsWith('gitops.') && r.target.includes(app.name)),
  )
}

export function hasGitOpsComparisonError(app: GitOpsApplicationView): boolean {
  if (app.primary_condition != null && app.primary_condition !== '') return true
  return (app.conditions ?? []).some(c => c.type === 'ComparisonError' || c.type === 'UnknownError')
}

export function syncPolicySummary(app: GitOpsApplicationView): string {
  if (!app.automated_sync) {
    return 'Manual sync only (automated sync disabled)'
  }
  const parts: string[] = ['Automated sync']
  if (app.self_heal) parts.push('selfHeal')
  if (app.prune) parts.push('prune')
  return parts.join(' · ')
}
