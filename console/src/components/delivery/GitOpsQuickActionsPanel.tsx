import { Button, ConfirmDialog, DenseTag } from '@bifrost/ui'
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { GitOpsApplicationView, GitOpsAppsResponse } from '@/api/types'
import { rollbackGitOpsApp, syncGitOpsApp } from '@/api/platform'
import { DeliveryBrandIcon } from '@/components/delivery/DeliveryBrandIcon'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { StatusLamp } from '@/components/StatusLamp'
import { gitOpsApplicationHints, hintTagVariant } from '@/lib/delivery/gitOpsApplicationHints'

interface GitOpsQuickActionsPanelProps {
  data: GitOpsAppsResponse | undefined
  isLoading: boolean
  errorMessage?: string | null
  onOpenObserve?: () => void
}

type GitOpsConfirmAction =
  | { kind: 'sync'; app: GitOpsApplicationView }
  | { kind: 'rollback'; app: GitOpsApplicationView }

function needsGitOpsAction(app: GitOpsApplicationView): boolean {
  const hints = gitOpsApplicationHints(app)
  return hints.sync.level === 'recommend' || hints.sync.level === 'blocked'
}

export function GitOpsQuickActionsPanel({
  data,
  isLoading,
  errorMessage,
  onOpenObserve,
}: GitOpsQuickActionsPanelProps) {
  const { canOperate, canAdmin } = usePlatformAuth()
  const qc = useQueryClient()
  const apps = data?.apps ?? []
  const actionable = apps.filter(needsGitOpsAction)
  const allSynced = apps.length > 0 && actionable.length === 0

  const [pendingApp, setPendingApp] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [confirmAction, setConfirmAction] = useState<GitOpsConfirmAction | null>(null)

  const syncMutation = useMutation({
    mutationFn: syncGitOpsApp,
    onMutate: name => {
      setActionError(null)
      setPendingApp(name)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['gitops', 'apps'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      setConfirmAction(null)
    },
    onError: (err: Error) => setActionError(err.message),
    onSettled: () => setPendingApp(null),
  })

  const rollbackMutation = useMutation({
    mutationFn: (name: string) => rollbackGitOpsApp(name),
    onMutate: name => {
      setActionError(null)
      setPendingApp(name)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['gitops', 'apps'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      setConfirmAction(null)
    },
    onError: (err: Error) => setActionError(err.message),
    onSettled: () => setPendingApp(null),
  })

  const pending = syncMutation.isPending || rollbackMutation.isPending

  return (
    <OpsSection
      title="GitOps — post-deliver"
      leading={<DeliveryBrandIcon id="argocd" variant="scope" />}
      description="Sync only when deliver changed GitOps manifests. Full probe and audit filters live on Observe."
      bodyPadding="default"
      overflow="visible"
    >
      {errorMessage != null && errorMessage !== '' && (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{errorMessage}</p>
      )}
      {actionError != null && actionError !== '' && (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{actionError}</p>
      )}

      {isLoading || data == null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading GitOps…</p>
      ) : apps.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No Argo CD applications — install Argo CD or check Observe.
        </p>
      ) : allSynced ? (
        <p className="m-0 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
          <StatusLamp value="ok" kind="reach" />
          <span>All {apps.length} application(s) synced — no GitOps action needed.</span>
          {onOpenObserve != null && (
            <button type="button" className="focus-strip-link text-dense-meta" onClick={onOpenObserve}>
              Open Observe
            </button>
          )}
        </p>
      ) : (
        <ul className="m-0 flex list-none flex-col gap-2 p-0">
          {actionable.map(app => {
            const hints = gitOpsApplicationHints(app)
            return (
              <li
                key={`${app.namespace}/${app.name}`}
                className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--secondary)]/30 px-2.5 py-2"
              >
                <span className="font-mono-tabular text-dense-label">{app.name}</span>
                <DenseTag variant={hintTagVariant(hints.sync.level) === 'neutral' ? 'warning' : hintTagVariant(hints.sync.level)}>
                  {app.sync_status}
                </DenseTag>
                <span className="text-dense-meta text-muted-foreground">{hints.sync.shortLabel}</span>
                {canOperate && hints.sync.level !== 'blocked' && (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={pending}
                    onClick={() => setConfirmAction({ kind: 'sync', app })}
                  >
                    {pendingApp === app.name && syncMutation.isPending ? 'Syncing…' : 'Sync'}
                  </Button>
                )}
                {canAdmin && (app.history_count ?? 0) >= 2 && (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={pending}
                    onClick={() => setConfirmAction({ kind: 'rollback', app })}
                  >
                    {pendingApp === app.name && rollbackMutation.isPending ? 'Rolling back…' : 'Rollback'}
                  </Button>
                )}
              </li>
            )
          })}
        </ul>
      )}

      {!allSynced && onOpenObserve != null && (
        <p className="m-0 mt-2 text-dense-meta text-muted-foreground">
          <button type="button" className="focus-strip-link" onClick={onOpenObserve}>
            Observe tab
          </button>
          {' '}— full Application table, drawer guidance, and pipeline history.
        </p>
      )}

      <ConfirmDialog
        open={confirmAction != null}
        title={confirmAction?.kind === 'rollback' ? 'Rollback Argo CD application' : 'Sync Argo CD application'}
        message={
          confirmAction != null
            ? confirmAction.kind === 'rollback'
              ? `Roll back ${confirmAction.app.name} to the previous deployed revision?`
              : `Sync ${confirmAction.app.name} to the latest Git revision?`
            : ''
        }
        confirmLabel={confirmAction?.kind === 'rollback' ? 'Rollback' : 'Sync'}
        confirming={pending}
        onConfirm={() => {
          if (confirmAction == null) return
          if (confirmAction.kind === 'rollback') {
            rollbackMutation.mutate(confirmAction.app.name)
            return
          }
          syncMutation.mutate(confirmAction.app.name)
        }}
        onCancel={() => setConfirmAction(null)}
      />
    </OpsSection>
  )
}
