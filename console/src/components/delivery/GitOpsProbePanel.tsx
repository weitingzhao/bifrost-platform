import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { ArgoCDStatus, GitOpsAppsResponse, Reachability } from '@/api/types'
import { syncGitOpsApp } from '@/api/platform'
import { DeliveryBrandIcon } from '@/components/delivery/DeliveryBrandIcon'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { StatusLamp } from '@/components/StatusLamp'

interface GitOpsProbePanelProps {
  data: GitOpsAppsResponse | undefined
  isLoading: boolean
  errorMessage?: string | null
}

function gitOpsHeadline(status: ArgoCDStatus | undefined, reachability?: Reachability): string {
  if (status === 'installed' && reachability === 'ok') {
    return 'Argo CD detected — Application CR probe active (P3 L0)'
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

export function GitOpsProbePanel({ data, isLoading, errorMessage }: GitOpsProbePanelProps) {
  const apps = data?.apps ?? []
  const { canOperate } = usePlatformAuth()
  const qc = useQueryClient()
  const [syncError, setSyncError] = useState<string | null>(null)
  const [syncingApp, setSyncingApp] = useState<string | null>(null)

  const syncMutation = useMutation({
    mutationFn: syncGitOpsApp,
    onMutate: name => {
      setSyncError(null)
      setSyncingApp(name)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['gitops', 'apps'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setSyncError(err.message),
    onSettled: () => setSyncingApp(null),
  })

  const headerExtra = (
    <>
      {errorMessage != null && errorMessage !== '' && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{errorMessage}</p>
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
      {syncError != null && syncError !== '' && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{syncError}</p>
      )}
    </>
  )

  return (
    <OpsSection
      title="GitOps — Argo CD probe"
      leading={<DeliveryBrandIcon id="argocd" variant="scope" />}
      actions={
        <span className="font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {isLoading ? '…' : `GET /api/v1/gitops/apps · ns ${data?.argocd_namespace ?? 'cicd'}`}
        </span>
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
            <DenseTableHead>Project</DenseTableHead>
            <DenseTableHead>Sync</DenseTableHead>
            <DenseTableHead>Health</DenseTableHead>
            <DenseTableHead>Destination</DenseTableHead>
            {canOperate && <DenseTableHead>Actions</DenseTableHead>}
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading || (data == null && errorMessage == null) ? (
            <DenseTableRow>
              <DenseTableCell colSpan={canOperate ? 6 : 5} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : apps.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={canOperate ? 6 : 5} className="text-[var(--muted-foreground)]">
                {data?.argocd_status === 'installed'
                  ? 'No Argo CD Application resources yet'
                  : 'Install Argo CD in cicd namespace to enable GitOps sync'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            apps.map(app => (
              <DenseTableRow key={`${app.namespace}/${app.name}`}>
                <DenseTableCell className="font-mono-tabular">{app.name}</DenseTableCell>
                <DenseTableCell>{app.project ?? '—'}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={syncTagVariant(app.sync_status)}>{app.sync_status}</DenseTag>
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={healthTagVariant(app.health_status)}>{app.health_status}</DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                  {app.destination ?? '—'}
                </DenseTableCell>
                {canOperate && (
                  <DenseTableCell>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={syncingApp === app.name}
                      onClick={() => syncMutation.mutate(app.name)}
                    >
                      {syncingApp === app.name ? 'Syncing…' : 'Sync'}
                    </Button>
                  </DenseTableCell>
                )}
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
