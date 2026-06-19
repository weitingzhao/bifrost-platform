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
  Input,
} from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { SupplyChainResponse } from '@/api/types'
import {
  fetchSupplyChain,
  refreshDockerfileConfigMaps,
  startPipelineRun,
  triggerMirrorSync,
} from '@/api/platform'
import { DockerfileCmSummary } from '@/components/delivery/DockerfileCmSummary'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { DELIVERY_FOCUS_RUN_QUERY_KEY } from '@/lib/delivery/deliveryFocusRun'
import { formatPipelineRunStatus, isPipelineRunSucceeded } from '@/lib/delivery/pipelineRunAskPack'

function runStatusVariant(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  const s = status.toLowerCase()
  if (s === 'true' || s === 'succeeded' || s === 'success') return 'success'
  if (s === 'false' || s === 'failed') return 'danger'
  if (s === 'unknown' || s === 'running') return 'warning'
  return 'neutral'
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`
  return `${(n / (1024 * 1024)).toFixed(1)} MiB`
}

export type SupplyChainPanelLayout = 'operate' | 'observe' | 'full'

interface SupplyChainPanelProps {
  /** operate: actuation bar only; observe: inventory tables; full: all sections */
  layout?: SupplyChainPanelLayout
}

export function SupplyChainPanel({ layout = 'full' }: SupplyChainPanelProps) {
  const { canOperate } = usePlatformAuth()
  const qc = useQueryClient()
  const [revision, setRevision] = useState('main')
  const [actionError, setActionError] = useState<string | null>(null)

  const supplyQuery = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 15_000,
  })

  const mirrorMutation = useMutation({
    mutationFn: triggerMirrorSync,
    onMutate: () => setActionError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['delivery', 'supply-chain'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const refreshMutation = useMutation({
    mutationFn: (rev: string) => refreshDockerfileConfigMaps(rev),
    onMutate: () => setActionError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['delivery', 'supply-chain'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const deliverMutation = useMutation({
    mutationFn: (rev: string) => startPipelineRun('bifrost-deliver-stg', rev),
    onMutate: () => setActionError(null),
    onSuccess: data => {
      if (data.run?.name) {
        qc.setQueryData(DELIVERY_FOCUS_RUN_QUERY_KEY, data.run.name)
        void qc.invalidateQueries({ queryKey: ['delivery', 'steps', data.run.name] })
      }
      void qc.invalidateQueries({ queryKey: ['promote', 'release-gate', 'stg'] })
      void qc.invalidateQueries({ queryKey: ['promote', 'tier-b'] })
      void qc.invalidateQueries({ queryKey: ['delivery', 'runs', 'bifrost-deliver-stg'] })
      void qc.invalidateQueries({ queryKey: ['delivery', 'supply-chain'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const data: SupplyChainResponse | undefined = supplyQuery.data
  const pending =
    mirrorMutation.isPending || refreshMutation.isPending || deliverMutation.isPending

  const showActuation = layout === 'operate' || layout === 'full'
  const showInventory = layout === 'observe' || layout === 'full'
  const sectionTitle =
    layout === 'operate' ? 'Supply chain — actuate' : layout === 'observe' ? 'Supply chain — inventory' : 'Supply chain'
  const sectionDescription =
    layout === 'operate'
      ? 'Push to GitHub first, then sync mirrors, refresh Dockerfile ConfigMaps, and run deliver-stg.'
      : layout === 'observe'
        ? 'Kaniko Dockerfile ConfigMaps and STG deployment images after deliver.'
        : 'Gitea mirror sources, Kaniko Dockerfile ConfigMaps, and STG deployment images. Manage code revision before deliver-stg.'

  return (
    <OpsSection
      title={sectionTitle}
      description={sectionDescription}
      actions={
        <span className="font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {supplyQuery.isLoading ? '…' : 'GET /api/v1/delivery/supply-chain'}
        </span>
      }
      headerExtra={
        <>
          {supplyQuery.error instanceof Error && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">
              {supplyQuery.error.message}
            </p>
          )}
          {actionError != null && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{actionError}</p>
          )}
          {!supplyQuery.isLoading && data != null && supplyQuery.error == null && (
            <p className="m-0 mt-2 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
              <StatusLamp value={data.reachability} kind="reach" />
              <span>{data.detail}</span>
              <DenseTag variant={data.mirror_credentials_configured ? 'success' : 'warning'}>
                mirror creds {data.mirror_credentials_configured ? 'OK' : 'missing'}
              </DenseTag>
            </p>
          )}
        </>
      }
      bodyPadding={showInventory ? 'none' : 'default'}
      overflow="visible"
      bodyClassName={showInventory ? 'ops-section-body--table' : undefined}
    >
      {showActuation && (
      <div className={showInventory ? 'border-b border-[var(--border)] px-3 py-2' : 'px-3 py-2'}>
        <OpsSubsectionTitle>Revision & actuation</OpsSubsectionTitle>
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground shrink-0">Revision:</span>
          <Input
            className="h-8 w-36 font-mono-tabular text-dense-meta"
            value={revision}
            onChange={e => setRevision(e.target.value)}
            placeholder="main"
            disabled={!canOperate || pending}
          />
          {canOperate && (
            <>
              <Button
                size="sm"
                variant="outline"
                disabled={pending}
                onClick={() => mirrorMutation.mutate()}
              >
                {mirrorMutation.isPending ? 'Syncing…' : 'Sync mirrors'}
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={pending}
                onClick={() => refreshMutation.mutate(revision.trim() || 'main')}
              >
                {refreshMutation.isPending ? 'Refreshing…' : 'Refresh Dockerfile CMs'}
              </Button>
              <Button
                size="sm"
                disabled={pending}
                onClick={() => deliverMutation.mutate(revision.trim() || 'main')}
              >
                {deliverMutation.isPending ? 'Starting…' : 'Run deliver-stg'}
              </Button>
            </>
          )}
        </div>
        {data?.last_supply_chain_task != null && (
          <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Last supply-chain task: {data.last_supply_chain_task.name} ({data.last_supply_chain_task.actuation}) —{' '}
            {data.last_supply_chain_task.status}
          </p>
        )}
        {layout === 'operate' && (
          <DockerfileCmSummary configmaps={data?.dockerfile_configmaps} loading={supplyQuery.isLoading} />
        )}
        {data?.last_deliver_run != null && layout !== 'operate' && (
          <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Last deliver: {data.last_deliver_run.name} —{' '}
            <DenseTag variant={runStatusVariant(data.last_deliver_run.status)} className="inline-flex">
              {formatPipelineRunStatus(data.last_deliver_run)}
            </DenseTag>
            {data.last_deliver_success != null && isPipelineRunSucceeded(data.last_deliver_success) && (
              <span> · last success {data.last_deliver_success.name}</span>
            )}
          </p>
        )}
      </div>
      )}

      {showInventory && (
      <>
      <div className="border-b border-[var(--border)] px-3 py-2">
        <OpsSubsectionTitle>
          Dockerfile ConfigMaps ({supplyQuery.isLoading ? '…' : data?.dockerfile_configmaps.length ?? 0})
        </OpsSubsectionTitle>
      </div>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>ConfigMap</DenseTableHead>
            <DenseTableHead>Present</DenseTableHead>
            <DenseTableHead>Files</DenseTableHead>
            <DenseTableHead>Size</DenseTableHead>
            <DenseTableHead>Resource version</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {supplyQuery.isLoading ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : (data?.dockerfile_configmaps.length ?? 0) === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                No Dockerfile ConfigMaps — run Refresh Dockerfile CMs or make k3s-deliver-stg
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            data!.dockerfile_configmaps.map(cm => (
              <DenseTableRow key={cm.name}>
                <DenseTableCell className="font-mono-tabular">{cm.name}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={cm.present ? 'success' : 'danger'}>
                    {cm.present ? 'yes' : 'no'}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                  {cm.file_keys?.join(', ') ?? '—'}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                  {cm.approx_bytes != null ? formatBytes(cm.approx_bytes) : '—'}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                  {cm.resource_version ?? cm.detail ?? '—'}
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>

      <div className="border-b border-t border-[var(--border)] px-3 py-2">
        <OpsSubsectionTitle>
          STG workload images ({supplyQuery.isLoading ? '…' : data?.stg_workloads.length ?? 0})
        </OpsSubsectionTitle>
      </div>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Deployment</DenseTableHead>
            <DenseTableHead>Image</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {supplyQuery.isLoading ? (
            <DenseTableRow>
              <DenseTableCell colSpan={2} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : (data?.stg_workloads.length ?? 0) === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={2} className="text-[var(--muted-foreground)]">
                No STG deployments found in {data?.stg_namespace ?? 'bifrost-stg'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            data!.stg_workloads.map(w => (
              <DenseTableRow key={w.deployment}>
                <DenseTableCell className="font-medium">{w.deployment}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)] break-all">
                  {w.image}
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>

      {data?.tracked_repos != null && data.tracked_repos.length > 0 && (
        <div className="border-t border-[var(--border)] px-3 py-2">
          <OpsSubsectionTitle>Tracked Gitea mirrors</OpsSubsectionTitle>
          <p className="m-0 mt-1 font-mono-tabular text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {data.tracked_repos.join(' · ')}
          </p>
        </div>
      )}
      </>
      )}
    </OpsSection>
  )
}
