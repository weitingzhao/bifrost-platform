import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import {
  deletePod,
  ensureBifrostNamespaces,
  ensureMetricsServer,
  fetchAudit,
  fetchCluster,
  fetchClusterEvents,
  fetchClusterMetrics,
  fetchClusterNamespaces,
  fetchClusterNodes,
  fetchClusterObservability,
  fetchClusterWorkloads,
  fetchPodLogs,
  rolloutRestartDeployment,
  scaleDeployment,
  syncClusterKubeconfig,
} from '@/api/platform'
import type { ClusterWorkload } from '@/api/types'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { ClusterDrawer } from '@/components/cluster/ClusterDrawer'
import { ClusterNamespacesPanel } from '@/components/cluster/ClusterNamespacesPanel'
import { ClusterNodesTable } from '@/components/cluster/ClusterNodesTable'
import { ClusterObservabilityPanel } from '@/components/cluster/ClusterObservabilityPanel'
import { ClusterOverviewKpi } from '@/components/cluster/ClusterOverviewKpi'
import { ClusterTopPodsTable } from '@/components/cluster/ClusterTopPodsTable'
import { ClusterWorkloadsTable } from '@/components/cluster/ClusterWorkloadsTable'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

type NsFilter = 'all' | 'bifrost'

interface ConfirmState {
  open: boolean
  title: string
  message: string
  confirmLabel: string
  action: () => void
}

interface ScaleState {
  workload: ClusterWorkload
  replicas: number
}

export function ClusterPage({
  onOpenStandards,
  onOpenEnvironments,
}: {
  onOpenStandards?: () => void
  onOpenEnvironments?: () => void
}) {
  const qc = useQueryClient()
  const [nsFilter, setNsFilter] = useState<NsFilter>('bifrost')
  const [selectedNs, setSelectedNs] = useState<string | null>('cicd')
  const [selectedPod, setSelectedPod] = useState<string | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [syncError, setSyncError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [confirmState, setConfirmState] = useState<ConfirmState | null>(null)
  const [scaleState, setScaleState] = useState<ScaleState | null>(null)

  const { canOperate, canAdmin, caps, capsLoading } = usePlatformAuth()

  const summaryQuery = useQuery({
    queryKey: ['cluster', 'summary'],
    queryFn: fetchCluster,
    refetchInterval: 30_000,
  })

  const nodesQuery = useQuery({
    queryKey: ['cluster', 'nodes'],
    queryFn: fetchClusterNodes,
    refetchInterval: 30_000,
  })

  const metricsQuery = useQuery({
    queryKey: ['cluster', 'metrics'],
    queryFn: () => fetchClusterMetrics(8),
    refetchInterval: 30_000,
  })

  const observabilityQuery = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 30_000,
    retry: false,
  })

  const namespacesQuery = useQuery({
    queryKey: ['cluster', 'namespaces', nsFilter],
    queryFn: () => fetchClusterNamespaces(nsFilter === 'bifrost' ? 'bifrost' : ''),
    refetchInterval: 30_000,
  })

  const workloadsQuery = useQuery({
    queryKey: ['cluster', 'workloads', selectedNs],
    queryFn: () => fetchClusterWorkloads(selectedNs ?? 'default'),
    enabled: selectedNs != null,
    refetchInterval: 30_000,
  })

  const eventsQuery = useQuery({
    queryKey: ['cluster', 'events', selectedNs],
    queryFn: () => fetchClusterEvents(selectedNs ?? undefined, 50),
    enabled: selectedNs != null && drawerOpen,
    refetchInterval: 30_000,
  })

  const logsQuery = useQuery({
    queryKey: ['cluster', 'pod-logs', selectedNs, selectedPod],
    queryFn: () => fetchPodLogs(selectedNs ?? '', selectedPod ?? '', 200),
    enabled: selectedNs != null && selectedPod != null && drawerOpen,
    refetchInterval: 30_000,
  })

  const auditQuery = useQuery({
    queryKey: ['platform', 'audit'],
    queryFn: fetchAudit,
    refetchInterval: 30_000,
  })

  const syncMutation = useMutation({
    mutationFn: syncClusterKubeconfig,
    onSuccess: data => {
      setSyncError(data.ok ? null : data.message)
      if (data.ok) {
        void qc.invalidateQueries({ queryKey: ['cluster'] })
      }
    },
    onError: (err: Error) => setSyncError(err.message),
  })

  const ensureMutation = useMutation({
    mutationFn: ensureBifrostNamespaces,
    onSuccess: data => handleActuationSuccess(data.message),
    onError: (err: Error) => setActionError(err.message),
  })

  const metricsServerMutation = useMutation({
    mutationFn: ensureMetricsServer,
    onSuccess: data => handleActuationSuccess(data.message),
    onError: (err: Error) => setActionError(err.message),
  })

  const restartMutation = useMutation({
    mutationFn: rolloutRestartDeployment,
    onSuccess: data => handleActuationSuccess(data.message),
    onError: (err: Error) => setActionError(err.message),
  })

  const scaleMutation = useMutation({
    mutationFn: scaleDeployment,
    onSuccess: data => {
      setScaleState(null)
      handleActuationSuccess(data.message)
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const deletePodMutation = useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) => deletePod(namespace, name),
    onSuccess: data => {
      setDrawerOpen(false)
      setSelectedPod(null)
      handleActuationSuccess(data.message)
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const selectedWorkload = useMemo(() => {
    if (selectedPod == null) return undefined
    return workloadsQuery.data?.workloads.find(w => w.name === selectedPod)
  }, [workloadsQuery.data, selectedPod])

  const podEvents = useMemo(() => {
    if (selectedPod == null) return []
    return (eventsQuery.data?.events ?? []).filter(e => e.object?.includes(selectedPod) ?? false)
  }, [eventsQuery.data, selectedPod])

  const auditRecords = auditQuery.data?.records ?? []

  const unreachable =
    summaryQuery.data?.reachability === 'fail' &&
    (summaryQuery.data?.detail?.includes('kubeconfig') ?? false)

  function refreshCluster() {
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
  }

  function handleActuationSuccess(message: string) {
    setActionError(null)
    setConfirmState(null)
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    setSyncError(message)
  }

  function handleSelectNs(name: string) {
    setSelectedNs(name)
    setSelectedPod(null)
    setDrawerOpen(false)
  }

  function handleSelectPod(name: string) {
    setSelectedPod(name)
    setDrawerOpen(true)
  }

  function requireConfirm(next: Omit<ConfirmState, 'open'>) {
    setActionError(null)
    setConfirmState({ ...next, open: true })
  }

  function handleEnsureMetricsServer() {
    requireConfirm({
      title: 'Install metrics-server',
      message:
        'This installs metrics-server in kube-system (Layer A). Required for live CPU/memory usage and top pods. Does not install Prometheus or Grafana (Layer B).',
      confirmLabel: 'Install metrics-server',
      action: () => metricsServerMutation.mutate(),
    })
  }

  function handleEnsureNamespaces() {
    requireConfirm({
      title: 'Ensure Bifrost namespaces',
      message: 'This creates missing Bifrost namespaces from clusters.yaml. Existing namespaces are left unchanged.',
      confirmLabel: 'Ensure namespaces',
      action: () => ensureMutation.mutate(),
    })
  }

  function handleRestartDeployment(workload: ClusterWorkload) {
    requireConfirm({
      title: 'Restart deployment',
      message: `This requests a Kubernetes rollout restart for ${workload.namespace}/${workload.name}.`,
      confirmLabel: 'Restart deployment',
      action: () =>
        restartMutation.mutate({
          namespace: workload.namespace,
          kind: 'Deployment',
          name: workload.name,
        }),
    })
  }

  function handleDeletePod(workload: ClusterWorkload) {
    requireConfirm({
      title: 'Delete pod',
      message: `This deletes pod ${workload.namespace}/${workload.name}. Its controller may create a replacement pod.`,
      confirmLabel: 'Delete pod',
      action: () => deletePodMutation.mutate({ namespace: workload.namespace, name: workload.name }),
    })
  }

  function actionPending() {
    return (
      ensureMutation.isPending ||
      metricsServerMutation.isPending ||
      restartMutation.isPending ||
      scaleMutation.isPending ||
      deletePodMutation.isPending
    )
  }

  const metricsOk = metricsQuery.data?.metrics_server_available === true

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <section className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              K3s cluster · bifrost-bootstrap · P1 actuation
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <button type="button" className="btn-ui" onClick={refreshCluster}>
              Refresh
            </button>
            <button
              type="button"
              className="btn-ui btn-ui-primary"
              disabled={syncMutation.isPending}
              onClick={() => {
                setSyncError(null)
                syncMutation.mutate()
              }}
            >
              {syncMutation.isPending ? 'Syncing…' : 'Sync kubeconfig'}
            </button>
          </div>
        </div>
        {syncError != null && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] lamp-fail">{syncError}</p>
        )}
        {syncMutation.data?.ok === true && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {syncMutation.data.message}
          </p>
        )}
        {actionError != null && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] lamp-warn">
            {actionError.includes('401') || actionError.includes('operator token required')
              ? 'Operator token required. Set PLATFORM_OPERATOR_TOKEN for the API and VITE_PLATFORM_OPERATOR_TOKEN for the console, then restart platform.'
              : actionError}
          </p>
        )}
      </section>

      <section className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="m-0 text-sm font-semibold">Actions</h2>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {canOperate
                ? `Authenticated as ${caps?.principal ?? 'operator'}${canAdmin ? ' (admin)' : ''}`
                : capsLoading
                  ? 'Checking operator session…'
                  : 'Use Authenticate in the header to enable write actions.'}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {!metricsOk && (
              <button
                type="button"
                className="btn-ui"
                disabled={!canAdmin || metricsServerMutation.isPending}
                onClick={handleEnsureMetricsServer}
              >
                {metricsServerMutation.isPending ? 'Installing…' : 'Install metrics-server'}
              </button>
            )}
            <button
              type="button"
              className="btn-ui btn-ui-primary"
              disabled={!canOperate || ensureMutation.isPending}
              onClick={handleEnsureNamespaces}
            >
              Ensure Bifrost namespaces
            </button>
          </div>
        </div>
        {!canOperate && !capsLoading && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Dev default token: <code>platform-operator-dev</code> (operator) or{' '}
            <code>platform-admin-dev</code> (admin + metrics-server). See{' '}
            <code>config/platform-auth.yaml</code>.
          </p>
        )}
        {!metricsOk && canOperate && !canAdmin && !capsLoading && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Install metrics-server requires admin token (<code>platform-admin-dev</code>).
          </p>
        )}
      </section>

      {unreachable && (
        <section className="page-section panel-elevated px-4 py-3 lamp-warn">
          <strong>Kubeconfig required.</strong> Run from infra repo:
          <pre className="mt-2 overflow-x-auto rounded bg-[var(--background)] p-2 text-[var(--text-dense-meta)] font-mono-tabular">
            {`cd bifrost-trade-infra && make k3s-fetch-kubeconfig
export PLATFORM_KUBECONFIG=$HOME/.kube/bifrost-k3s.yaml
cd ../bifrost-platform && make start`}
          </pre>
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Or click <strong>Sync kubeconfig</strong> above (requires PLATFORM_CLUSTER_SYNC_ENABLED=1).
          </p>
        </section>
      )}

      <ClusterOverviewKpi
        summary={summaryQuery.data}
        metrics={metricsQuery.data}
        isLoading={summaryQuery.isLoading || metricsQuery.isLoading}
      />

      <ClusterNodesTable
        nodes={nodesQuery.data?.nodes ?? []}
        isLoading={nodesQuery.isLoading}
        metricsAvailable={metricsQuery.data?.metrics_server_available}
      />

      <div className="grid gap-4 lg:grid-cols-2">
        <ClusterNamespacesPanel
          namespaces={namespacesQuery.data?.namespaces ?? []}
          filter={nsFilter}
          selectedNs={selectedNs}
          isLoading={namespacesQuery.isLoading}
          onFilterChange={filter => {
            setNsFilter(filter)
            setSelectedPod(null)
            setDrawerOpen(false)
          }}
          onSelectNs={handleSelectNs}
        />
        <ClusterWorkloadsTable
          namespace={selectedNs}
          workloads={workloadsQuery.data?.workloads ?? []}
          isLoading={workloadsQuery.isLoading}
          selectedPod={selectedPod}
          onSelectPod={handleSelectPod}
          onRestartDeployment={handleRestartDeployment}
          onScaleDeployment={workload => setScaleState({ workload, replicas: 1 })}
          onDeletePod={handleDeletePod}
        />
      </div>

      <ClusterTopPodsTable metrics={metricsQuery.data} isLoading={metricsQuery.isLoading} />

      <ClusterObservabilityPanel
        data={observabilityQuery.data}
        isLoading={observabilityQuery.isLoading}
        onOpenStandards={onOpenStandards}
        onOpenEnvironments={onOpenEnvironments}
      />

      <section className="page-section panel-elevated overflow-hidden">
        <header className="flex items-center justify-between gap-2 border-b border-[var(--border)] px-3 py-2">
          <h2 className="m-0 text-sm font-semibold">Audit</h2>
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {auditQuery.isLoading ? '…' : `${auditRecords.length} records`}
          </span>
        </header>
        <div className="dense-table-scroll">
          <table className="dense-table">
            <thead>
              <tr>
                <th>Time</th>
                <th>Actor</th>
                <th>Action</th>
                <th>Target</th>
                <th>Status</th>
                <th>Detail</th>
              </tr>
            </thead>
            <tbody>
              {auditRecords.length === 0 ? (
                <tr>
                  <td colSpan={6} className="text-[var(--muted-foreground)]">
                    {auditQuery.isLoading ? 'Loading…' : 'No actuation records yet'}
                  </td>
                </tr>
              ) : (
                auditRecords.slice(0, 20).map(record => (
                  <tr key={record.id}>
                    <td className="font-mono-tabular">{new Date(record.at).toLocaleString()}</td>
                    <td className="font-mono-tabular">{record.actor}</td>
                    <td className="font-mono-tabular">{record.action}</td>
                    <td className="font-mono-tabular">{record.target}</td>
                    <td className="font-mono-tabular">{record.status}</td>
                    <td>{record.detail}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      <ClusterDrawer
        open={drawerOpen}
        namespace={selectedNs}
        podName={selectedPod}
        workload={selectedWorkload}
        events={podEvents}
        eventsLoading={eventsQuery.isLoading}
        logs={logsQuery.data?.logs}
        logsLoading={logsQuery.isLoading}
        logsError={logsQuery.error instanceof Error ? logsQuery.error.message : null}
        onClose={() => {
          setDrawerOpen(false)
          setSelectedPod(null)
        }}
      />

      <ConfirmDialog
        open={confirmState?.open === true}
        title={confirmState?.title ?? ''}
        message={confirmState?.message ?? ''}
        confirmLabel={confirmState?.confirmLabel}
        confirming={actionPending()}
        onConfirm={() => confirmState?.action()}
        onCancel={() => setConfirmState(null)}
      />

      {scaleState != null && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4" role="presentation">
          <div className="w-full max-w-md rounded-lg border border-[var(--border)] bg-[var(--card)] p-4 shadow-xl">
            <h2 className="m-0 text-base font-semibold">Scale deployment</h2>
            <p className="mt-2 text-sm text-[var(--muted-foreground)]">
              Set replicas for {scaleState.workload.namespace}/{scaleState.workload.name}.
            </p>
            <label className="mt-3 block text-sm">
              Replicas
              <input
                className="mt-1 w-full rounded border border-[var(--border)] bg-[var(--background)] px-2 py-1 font-mono-tabular"
                type="number"
                min={0}
                max={20}
                value={scaleState.replicas}
                onChange={event =>
                  setScaleState({
                    ...scaleState,
                    replicas: Number(event.currentTarget.value),
                  })
                }
              />
            </label>
            <div className="mt-4 flex justify-end gap-2">
              <button type="button" className="btn-ui" onClick={() => setScaleState(null)}>
                Cancel
              </button>
              <button
                type="button"
                className="btn-ui btn-ui-primary"
                disabled={scaleMutation.isPending}
                onClick={() =>
                  scaleMutation.mutate({
                    namespace: scaleState.workload.namespace,
                    kind: 'Deployment',
                    name: scaleState.workload.name,
                    replicas: scaleState.replicas,
                  })
                }
              >
                {scaleMutation.isPending ? 'Scaling…' : 'Scale deployment'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
