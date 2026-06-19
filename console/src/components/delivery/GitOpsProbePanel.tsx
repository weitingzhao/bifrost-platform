import {
  Button,
  ConfirmDialog,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  denseTableEntityLink,
} from '@bifrost/ui'
import { useEffect, useState } from 'react'
import { useIsFetching, useMutation, useQueryClient } from '@tanstack/react-query'
import type { ArgoCDStatus, GitOpsApplicationView, GitOpsAppsResponse, Reachability } from '@/api/types'
import { rollbackGitOpsApp, syncGitOpsApp } from '@/api/platform'
import { GitOpsApplicationDrawer } from '@/components/delivery/GitOpsApplicationDrawer'
import { DeliveryBrandIcon } from '@/components/delivery/DeliveryBrandIcon'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { StatusLamp } from '@/components/StatusLamp'
import { gitOpsApplicationHints, hasGitOpsComparisonError, hintTagVariant } from '@/lib/delivery/gitOpsApplicationHints'

interface GitOpsProbePanelProps {
  data: GitOpsAppsResponse | undefined
  isLoading: boolean
  errorMessage?: string | null
  layout?: 'observe' | 'operate'
  onOpenAudit?: () => void
  onDrawerOpenChange?: (open: boolean) => void
}

type GitOpsConfirmAction =
  | { kind: 'sync'; app: GitOpsApplicationView }
  | { kind: 'rollback'; app: GitOpsApplicationView; revision?: string }

function gitOpsHeadline(status: ArgoCDStatus | undefined, reachability?: Reachability): string {
  if (status === 'installed' && reachability === 'ok') {
    return 'Argo CD detected — click an Application for Git source, policy, and action guidance (P3)'
  }
  if (status === 'installed') {
    return 'Argo CD detected — partial (CRD or server not fully ready)'
  }
  switch (status) {
    case 'degraded':
    case 'unavailable':
      return 'Argo CD unavailable — check kubeconfig or cluster reachability'
    case 'not_installed':
      return 'Planned — Argo CD not installed in cicd namespace'
    default:
      return 'GitOps probe pending'
  }
}

function gitOpsLamp(reachability: Reachability | undefined, status: ArgoCDStatus | undefined) {
  if (reachability === 'fail' || status === 'unavailable') return 'fail' as const
  if (reachability === 'degraded' || status === 'not_installed') return 'degraded' as const
  if (reachability === 'ok' && status === 'installed') return 'ok' as const
  return 'unknown' as const
}

function syncTagVariant(sync: string): 'success' | 'warning' | 'neutral' {
  const s = sync.toLowerCase()
  if (s === 'synced') return 'success'
  if (s === 'outofsync') return 'warning'
  return 'neutral'
}

function healthTagVariant(health: string): 'success' | 'warning' | 'danger' | 'neutral' {
  const h = health.toLowerCase()
  if (h === 'healthy') return 'success'
  if (h === 'degraded' || h === 'progressing') return 'warning'
  if (h === 'missing' || h === 'suspended') return 'danger'
  return 'neutral'
}

function shortRevision(revision: string | undefined): string {
  if (revision == null || revision === '') return '—'
  if (revision.length <= 12) return revision
  return `${revision.slice(0, 7)}…`
}

function confirmMessageForAction(action: GitOpsConfirmAction): string {
  const hints = gitOpsApplicationHints(action.app)
  if (action.kind === 'rollback') {
    return `${hints.rollback.message}\n\nProceed with rollback for ${action.app.name}?`
  }
  return `${hints.sync.message}\n\nProceed with sync for ${action.app.name}?`
}

export function GitOpsProbePanel({
  data,
  isLoading,
  errorMessage,
  layout = 'observe',
  onOpenAudit,
  onDrawerOpenChange,
}: GitOpsProbePanelProps) {
  const apps = data?.apps ?? []
  const { canOperate, canAdmin } = usePlatformAuth()
  const qc = useQueryClient()
  const gitopsFetching = useIsFetching({ queryKey: ['gitops', 'apps'] }) > 0
  const [actionError, setActionError] = useState<string | null>(null)
  const [pendingApp, setPendingApp] = useState<string | null>(null)
  const [confirmAction, setConfirmAction] = useState<GitOpsConfirmAction | null>(null)
  const [selectedApp, setSelectedApp] = useState<GitOpsApplicationView | null>(null)

  useEffect(() => {
    onDrawerOpenChange?.(selectedApp != null)
  }, [onDrawerOpenChange, selectedApp])

  useEffect(() => {
    setSelectedApp(current => {
      if (current == null) return null
      return apps.find(a => a.namespace === current.namespace && a.name === current.name) ?? current
    })
  }, [apps])

  const syncMutation = useMutation({
    mutationFn: syncGitOpsApp,
    onMutate: name => {
      setActionError(null)
      setPendingApp(name)
    },
    onSuccess: (_data, name) => {
      void qc.invalidateQueries({ queryKey: ['gitops', 'apps'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      setConfirmAction(null)
      const app = apps.find(a => a.name === name)
      if (app != null) setSelectedApp(app)
    },
    onError: (err: Error) => setActionError(err.message),
    onSettled: () => setPendingApp(null),
  })

  const rollbackMutation = useMutation({
    mutationFn: ({ appName, revision }: { appName: string; revision?: string }) =>
      rollbackGitOpsApp(appName, revision),
    onMutate: ({ appName }) => {
      setActionError(null)
      setPendingApp(appName)
    },
    onSuccess: (_data, { appName }) => {
      void qc.invalidateQueries({ queryKey: ['gitops', 'apps'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      setConfirmAction(null)
      const app = apps.find(a => a.name === appName)
      if (app != null) setSelectedApp(app)
    },
    onError: (err: Error) => setActionError(err.message),
    onSettled: () => setPendingApp(null),
  })

  const actionPending = syncMutation.isPending || rollbackMutation.isPending
  const showActions = layout === 'operate' && (canOperate || canAdmin)
  const comparisonErrorApps = apps.filter(hasGitOpsComparisonError)

  const confirmTitle =
    confirmAction?.kind === 'rollback' ? 'Rollback Argo CD application' : 'Sync Argo CD application'

  const openConfirm = (action: GitOpsConfirmAction) => {
    setSelectedApp(action.app)
    setConfirmAction(action)
  }

  const headerExtra = (
    <>
      {errorMessage != null && errorMessage !== '' && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] lamp-fail">{errorMessage}</p>
      )}
      {!isLoading && data != null && errorMessage == null && (
        <p className="m-0 mt-2 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
          <StatusLamp value={gitOpsLamp(data.reachability, data.argocd_status)} kind="reach" />
          <span>{gitOpsHeadline(data.argocd_status, data.reachability)}</span>
        </p>
      )}
      {!isLoading && data != null && data.detail !== '' && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{data.detail}</p>
      )}
      {data?.server != null && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Server: {data.server.kind}/{data.server.name} · {data.server.ready} ·{' '}
          <StatusLamp value={data.server.reachability} kind="reach" /> {data.server.status}
        </p>
      )}
      {layout === 'operate' && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Click an Application name for Git source, sync policy, Sync/Rollback guidance, and filtered audit.
        </p>
      )}
      {comparisonErrorApps.length > 0 && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] lamp-fail">
          {comparisonErrorApps.length} application(s) report Argo CD ComparisonError — Sync will fail until RBAC or
          repo access is fixed. Open the Application drawer for the full condition message.
        </p>
      )}
      {actionError != null && actionError !== '' && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] lamp-fail">{actionError}</p>
      )}
    </>
  )

  return (
    <>
      <OpsSection
        title={layout === 'operate' ? 'GitOps — Argo CD execution' : 'GitOps — Argo CD probe'}
        leading={<DeliveryBrandIcon id="argocd" variant="scope" />}
        actions={
          <SectionRefreshButton
            isFetching={gitopsFetching || isLoading}
            onClick={() => void qc.invalidateQueries({ queryKey: ['gitops', 'apps'] })}
          />
        }
        headerExtra={headerExtra}
        bodyPadding="none"
        overflow="visible"
        bodyClassName="ops-section-body--table"
      >
        <div className="border-b border-[var(--border)] px-3 py-2">
          <OpsSubsectionTitle>
            Argo CD Applications ({isLoading ? '…' : apps.length})
          </OpsSubsectionTitle>
        </div>

        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Application</DenseTableHead>
              <DenseTableHead>Sync</DenseTableHead>
              <DenseTableHead>Health</DenseTableHead>
              <DenseTableHead>Guidance</DenseTableHead>
              <DenseTableHead>Revision</DenseTableHead>
              <DenseTableHead>Destination</DenseTableHead>
              {showActions && <DenseTableHead>Actions</DenseTableHead>}
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {isLoading || (data == null && errorMessage == null) ? (
              <DenseTableRow>
                <DenseTableCell colSpan={showActions ? 7 : 6} className="text-[var(--muted-foreground)]">
                  Loading…
                </DenseTableCell>
              </DenseTableRow>
            ) : apps.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={showActions ? 7 : 6} className="text-[var(--muted-foreground)]">
                  {data?.argocd_status === 'installed'
                    ? 'No Argo CD Application resources yet'
                    : 'Install Argo CD in cicd namespace to enable GitOps sync'}
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              apps.map(app => {
                const hints = gitOpsApplicationHints(app)
                const selected =
                  selectedApp?.name === app.name && selectedApp.namespace === app.namespace
                return (
                  <DenseTableRow key={`${app.namespace}/${app.name}`}>
                    <DenseTableCell>
                      <button
                        type="button"
                        className={`${denseTableEntityLink} font-mono-tabular text-left`}
                        onClick={() => setSelectedApp(app)}
                      >
                        {app.name}
                      </button>
                      {selected && (
                        <span className="ml-2 text-[var(--text-dense-caption)] text-[var(--primary)]">
                          open
                        </span>
                      )}
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={syncTagVariant(app.sync_status)}>{app.sync_status}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={healthTagVariant(app.health_status)}>{app.health_status}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={hintTagVariant(hints.sync.level)}>{hints.sync.shortLabel}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                      {shortRevision(app.revision)}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                      {app.destination_namespace ?? app.destination ?? '—'}
                    </DenseTableCell>
                    {showActions && (
                      <DenseTableCell>
                        <div className="flex flex-wrap items-center gap-1.5">
                          {canOperate && (
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={actionPending}
                              onClick={() => openConfirm({ kind: 'sync', app })}
                            >
                              {pendingApp === app.name && syncMutation.isPending ? 'Syncing…' : 'Sync'}
                            </Button>
                          )}
                          {canAdmin && (
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={actionPending || hints.rollback.level === 'blocked'}
                              onClick={() => openConfirm({ kind: 'rollback', app })}
                            >
                              {pendingApp === app.name && rollbackMutation.isPending
                                ? 'Rolling back…'
                                : 'Rollback'}
                            </Button>
                          )}
                        </div>
                      </DenseTableCell>
                    )}
                  </DenseTableRow>
                )
              })
            )}
          </DenseTableBody>
        </DenseDataTable>

        <ConfirmDialog
          open={confirmAction != null}
          title={confirmTitle}
          message={confirmAction != null ? confirmMessageForAction(confirmAction) : ''}
          confirmLabel={confirmAction?.kind === 'rollback' ? 'Rollback' : 'Sync'}
          confirming={actionPending}
          onConfirm={() => {
            if (confirmAction == null) return
            if (confirmAction.kind === 'sync') {
              syncMutation.mutate(confirmAction.app.name)
              return
            }
            rollbackMutation.mutate({
              appName: confirmAction.app.name,
              revision: confirmAction.revision,
            })
          }}
          onCancel={() => setConfirmAction(null)}
        />
      </OpsSection>

      {selectedApp != null && (
        <GitOpsApplicationDrawer
          app={selectedApp}
          canOperate={layout === 'operate' && canOperate}
          canAdmin={layout === 'operate' && canAdmin}
          actionPending={actionPending}
          onClose={() => setSelectedApp(null)}
          onSync={() => openConfirm({ kind: 'sync', app: selectedApp })}
          onRollback={() => openConfirm({ kind: 'rollback', app: selectedApp })}
          onOpenAudit={onOpenAudit}
        />
      )}
    </>
  )
}
