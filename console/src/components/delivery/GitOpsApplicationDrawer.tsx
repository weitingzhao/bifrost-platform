import { Button, DenseTag } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import type { GitOpsApplicationView } from '@/api/types'
import { fetchAudit } from '@/api/platform'
import { AuditRecordsPanel } from '@/components/AuditRecordsPanel'
import {
  auditTargetForApp,
  filterAuditForApp,
  gitOpsApplicationHints,
  hintTagVariant,
  syncPolicySummary,
} from '@/lib/delivery/gitOpsApplicationHints'
import {
  classifyArgoOperation,
  opsOutcomeTextClass,
  opsOutcomeTextClassFromValue,
} from '@/lib/opsSemanticText'

interface GitOpsApplicationDrawerProps {
  app: GitOpsApplicationView
  canOperate: boolean
  canAdmin: boolean
  actionPending: boolean
  onClose: () => void
  onSync: () => void
  onRollback: () => void
  onOpenAudit?: () => void
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[var(--text-dense-caption)] uppercase tracking-wide text-[var(--muted-foreground)]">
        {label}
      </span>
      <span className="font-mono-tabular text-[var(--text-dense-meta)] break-all">{value}</span>
    </div>
  )
}

function HintBlock({
  title,
  hint,
}: {
  title: string
  hint: ReturnType<typeof gitOpsApplicationHints>['sync']
}) {
  return (
    <div className="rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <div className="mb-1 flex flex-wrap items-center gap-2">
        <span className="text-[var(--text-dense-label)] font-medium">{title}</span>
        <DenseTag variant={hintTagVariant(hint.level)}>{hint.shortLabel}</DenseTag>
      </div>
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{hint.message}</p>
    </div>
  )
}

export function GitOpsApplicationDrawer({
  app,
  canOperate,
  canAdmin,
  actionPending,
  onClose,
  onSync,
  onRollback,
  onOpenAudit,
}: GitOpsApplicationDrawerProps) {
  const hints = gitOpsApplicationHints(app)
  const operationOutcome = classifyArgoOperation(app.operation_phase ?? '', app.operation_message)
  const auditQuery = useQuery({
    queryKey: ['platform', 'audit'],
    queryFn: fetchAudit,
    refetchInterval: 15_000,
  })
  const appAudit = filterAuditForApp(auditQuery.data?.records ?? [], app)

  return (
    <aside className="bay-detail-drawer panel-elevated" role="dialog" aria-label={`Application ${app.name}`}>
      <header className="bay-detail-drawer-header">
        <div>
          <h3 className="m-0 text-sm font-semibold font-mono-tabular">{app.name}</h3>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Argo CD Application · {app.namespace}
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={onClose}>
          Close
        </Button>
      </header>

      <div className="bay-detail-drawer-body flex flex-col gap-4">
        <section>
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Git source
          </h4>
          <div className="flex flex-col gap-2">
            <DetailRow label="Repository" value={app.source_repo ?? '—'} />
            <DetailRow label="Path" value={app.source_path ?? '—'} />
            <DetailRow label="Target revision" value={app.source_target_revision ?? 'HEAD (default)'} />
          </div>
        </section>

        <section>
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Deployment target
          </h4>
          <div className="flex flex-col gap-2">
            <DetailRow label="Namespace" value={app.destination_namespace ?? app.destination ?? '—'} />
            <DetailRow label="Cluster destination" value={app.destination ?? '—'} />
          </div>
        </section>

        <section>
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Sync policy
          </h4>
          <p className="m-0 text-[var(--text-dense-meta)]">{syncPolicySummary(app)}</p>
        </section>

        <section>
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Observed state
          </h4>
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant={app.sync_status.toLowerCase() === 'synced' ? 'success' : 'warning'}>
              Sync: {app.sync_status}
            </DenseTag>
            <DenseTag
              variant={
                app.health_status.toLowerCase() === 'healthy'
                  ? 'success'
                  : app.health_status.toLowerCase() === 'degraded' ||
                      app.health_status.toLowerCase() === 'progressing'
                    ? 'warning'
                    : app.health_status.toLowerCase() === 'missing' ||
                        app.health_status.toLowerCase() === 'suspended'
                      ? 'danger'
                      : 'neutral'
              }
            >
              Health: {app.health_status}
            </DenseTag>
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              History: {app.history_count ?? 0}
            </span>
          </div>
          {app.revision != null && app.revision !== '' && (
            <p className="m-0 mt-2 font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Deployed revision: {app.revision}
            </p>
          )}
          {app.operation_phase != null && app.operation_phase !== '' && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Last operation:{' '}
              <span className={`font-mono-tabular ${opsOutcomeTextClassFromValue(app.operation_phase)}`}>
                {app.operation_phase}
              </span>
              {app.operation_message != null && app.operation_message !== '' && (
                <span className={`block mt-1 ${opsOutcomeTextClass(operationOutcome)}`}>
                  {app.operation_message}
                </span>
              )}
            </p>
          )}
        </section>

        {(app.conditions?.length ?? 0) > 0 && (
          <section>
            <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              Argo CD conditions
            </h4>
            <div className="flex flex-col gap-2">
              {app.conditions?.map(condition => (
                <div
                  key={`${condition.type}-${condition.last_transition_time ?? condition.message.slice(0, 32)}`}
                  className="rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2"
                >
                  <div className="mb-1 flex flex-wrap items-center gap-2">
                    <DenseTag
                      variant={
                        condition.type === 'ComparisonError' || condition.type === 'SyncError'
                          ? 'danger'
                          : 'warning'
                      }
                    >
                      {condition.type}
                    </DenseTag>
                    {condition.last_transition_time != null && condition.last_transition_time !== '' && (
                      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                        {condition.last_transition_time}
                      </span>
                    )}
                  </div>
                  <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] break-words">
                    {condition.message}
                  </p>
                </div>
              ))}
            </div>
            {app.primary_condition != null && app.primary_condition !== '' && (
              <p className="m-0 mt-2 text-[var(--text-dense-meta)] lamp-fail">
                Summary: {app.primary_condition}
              </p>
            )}
          </section>
        )}

        <section>
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            When to act
          </h4>
          <div className="flex flex-col gap-2">
            <HintBlock title="Sync" hint={hints.sync} />
            <HintBlock title="Rollback" hint={hints.rollback} />
          </div>
        </section>

        {(canOperate || canAdmin) && (
          <section>
            <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              Actions
            </h4>
            <div className="flex flex-wrap gap-2">
              {canOperate && (
                <Button size="sm" variant="outline" disabled={actionPending} onClick={onSync}>
                  Sync
                </Button>
              )}
              {canAdmin && (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={actionPending || hints.rollback.level === 'blocked'}
                  onClick={onRollback}
                >
                  Rollback
                </Button>
              )}
            </div>
            {hints.sync.level === 'noop' && canOperate && (
              <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Sync is allowed but likely a no-op while Synced + Healthy.
              </p>
            )}
            {hints.rollback.level === 'blocked' && canAdmin && (
              <p className="m-0 mt-2 text-[var(--text-dense-meta)] lamp-degraded">
                Rollback unavailable until a second deployment is recorded in history.
              </p>
            )}
          </section>
        )}

        <section>
          <div className="mb-2 flex items-center justify-between gap-2">
            <h4 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              Actuation audit
            </h4>
            <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {auditTargetForApp(app)}
            </span>
          </div>
          {auditQuery.isLoading ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading audit…</p>
          ) : appAudit.length === 0 ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              No platform-api actuation records for this Application yet. After Sync or Rollback, entries appear here
              with action <span className="font-mono-tabular">gitops.sync</span> or{' '}
              <span className="font-mono-tabular">gitops.rollback</span>.
            </p>
          ) : (
            <AuditRecordsPanel
              records={appAudit}
              isLoading={auditQuery.isLoading}
              limit={8}
              title="Recent actions"
              onViewAll={onOpenAudit}
            />
          )}
        </section>
      </div>
    </aside>
  )
}
