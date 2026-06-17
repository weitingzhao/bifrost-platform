import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import type { ArgoCDStatus, GitOpsAppsResponse, Reachability } from '@/api/types'
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

  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="border-b border-[var(--border)] px-3 py-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="m-0 text-sm font-semibold">GitOps — Argo CD probe</h2>
          <span className="font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {isLoading ? '…' : `GET /api/v1/gitops/apps · ns ${data?.argocd_namespace ?? 'cicd'}`}
          </span>
        </div>
        {errorMessage != null && errorMessage !== '' && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">
            {errorMessage}
          </p>
        )}
        {!isLoading && data != null && errorMessage == null && (
          <p className="m-0 mt-2 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
            <StatusLamp value={gitOpsLamp(data.reachability, data.argocd_status)} kind="reach" />
            <span>{gitOpsHeadline(data.argocd_status, data.reachability)}</span>
          </p>
        )}
        {!isLoading && data != null && data.detail !== '' && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {data.detail}
          </p>
        )}
        {data?.server != null && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Server: {data.server.kind}/{data.server.name} · {data.server.ready} ·{' '}
            <StatusLamp value={data.server.reachability} kind="reach" /> {data.server.status}
          </p>
        )}
      </header>

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Application</DenseTableHead>
            <DenseTableHead>Project</DenseTableHead>
            <DenseTableHead>Sync</DenseTableHead>
            <DenseTableHead>Health</DenseTableHead>
            <DenseTableHead>Destination</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading || (data == null && errorMessage == null) ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : apps.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
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
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </section>
  )
}
