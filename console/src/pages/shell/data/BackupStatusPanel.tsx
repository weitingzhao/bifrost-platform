import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { fetchClusterPostgresBackupStatus } from '@/api/cluster'
import { fetchEscapeHatch } from '@/api/core'
import { OpsSection } from '@/components/layout/OpsSection'

function when(iso: string | undefined): string {
  if (iso == null || iso === '') return '—'
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) return iso
  return parsed.toLocaleString()
}

/** Postgres backup freshness, plus the recorded escape-hatch drill time. */
export function BackupStatusPanel() {
  const backupQ = useQuery({
    queryKey: ['cluster', 'postgres', 'backup-status'],
    queryFn: fetchClusterPostgresBackupStatus,
    refetchInterval: 60_000,
  })
  const drillQ = useQuery({
    queryKey: ['platform', 'escape-hatch'],
    queryFn: fetchEscapeHatch,
    refetchInterval: 60_000,
  })
  const backup = backupQ.data
  const quarterly = drillQ.data?.quarterly

  return (
    <OpsSection
      title="Backup and drill"
      description="CNPG backup freshness from /api/v1/cluster/postgres/backup-status. Escape-hatch drill time is the last recorded exercise, not a pass/fail result."
      bodyPadding="default"
      overflow="visible"
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex flex-col gap-1">
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">Postgres backup</span>
          {backupQ.isLoading ? (
            <span className="text-sm">Loading…</span>
          ) : backupQ.isError ? (
            <span className="text-sm text-destructive">
              {backupQ.error instanceof Error ? backupQ.error.message : 'Backup status failed'}
            </span>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <DenseTag variant={backup?.fresh ? 'success' : backup?.signal === 'fail' ? 'danger' : 'warning'}>
                  {backup?.signal ?? 'unknown'}
                </DenseTag>
                <span className="text-sm">{backup?.detail ?? '—'}</span>
              </div>
              <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                Last completed {when(backup?.last_completed_at)}
                {backup?.last_backup_name != null && backup.last_backup_name !== ''
                  ? ` · ${backup.last_backup_name}`
                  : ''}
                {backup?.last_backup_phase != null && backup.last_backup_phase !== ''
                  ? ` · ${backup.last_backup_phase}`
                  : ''}
                {backup?.age_hours != null ? ` · ${backup.age_hours.toFixed(1)}h` : ''}
              </p>
            </>
          )}
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            Last recorded escape-hatch drill
          </span>
          {drillQ.isLoading ? (
            <span className="text-sm">Loading…</span>
          ) : drillQ.isError ? (
            <span className="text-sm text-destructive">
              {drillQ.error instanceof Error ? drillQ.error.message : 'Drill status failed'}
            </span>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <DenseTag variant={quarterly?.overdue ? 'warning' : 'neutral'}>
                  {quarterly?.overdue ? 'overdue' : 'recorded'}
                </DenseTag>
                <span className="text-sm">{when(quarterly?.last_drill_at)}</span>
              </div>
              <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                {quarterly?.last_drill_by != null && quarterly.last_drill_by !== ''
                  ? `By ${quarterly.last_drill_by}`
                  : 'No drill recorded yet'}
                {quarterly?.days_since_last_drill != null
                  ? ` · ${quarterly.days_since_last_drill}d ago`
                  : ''}
              </p>
            </>
          )}
        </div>
      </div>
    </OpsSection>
  )
}
