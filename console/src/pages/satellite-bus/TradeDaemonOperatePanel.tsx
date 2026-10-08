import { useMemo } from 'react'
import { Button, DenseTag, cn } from '@bifrost/ui'
import type { ClusterWorkload } from '@/api/clusterTypes'
import { isActivityInFlight } from '@/lib/activity/activityPageFocus'
import { useActivityFeed } from '@/lib/activity/activityStore'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import {
  RequestRestartDeployment,
  RequestScaleDeployment,
} from '@/components/shell/clusterActionRequests'
import { OpsSection } from '@/components/layout/OpsSection'
import { formatWorkloadRollout } from '@/lib/cluster/workloadRollout'
import type { TradeEnv } from '@/pages/satellite-bus/useSatelliteBusQueries'

function parseDesiredReplicas(ready: string | undefined): number | null {
  if (ready == null || ready === '—' || ready === '') return null
  const parts = ready.split('/')
  if (parts.length !== 2) return null
  const desired = Number(parts[1])
  return Number.isFinite(desired) ? desired : null
}

function findWorkload(workloads: ClusterWorkload[], name: string): ClusterWorkload | undefined {
  return workloads.find(w => w.name === name && w.kind.toLowerCase().includes('deploy'))
}

function WorkloadRolloutMeta({
  workload,
  loading,
}: {
  workload: ClusterWorkload | undefined
  loading: boolean
}) {
  if (loading) {
    return <span className="font-mono text-[var(--text-dense-meta)] text-muted-foreground">…</span>
  }
  const ready = workload?.ready ?? '—'
  const rollout = formatWorkloadRollout(workload)
  const progressing = workload?.status === 'Progressing' || workload?.status === 'Unavailable'
  return (
    <>
      <span className="font-mono text-[var(--text-dense-meta)] text-muted-foreground" title="Ready/desired replicas">
        {ready}
      </span>
      {progressing && workload?.status != null && (
        <DenseTag variant="warning" className="text-[9px] uppercase tracking-wide">
          {workload.status}
        </DenseTag>
      )}
      {rollout != null && (
        <span
          className="font-mono text-[var(--text-dense-caption)] text-muted-foreground"
          title="K8s Deployment rollout (updated · ready · available)"
        >
          {rollout}
        </span>
      )}
    </>
  )
}

export function TradeDaemonOperatePanel({
  tradeEnv,
  namespace,
  canOperate,
  workloads,
  workloadsLoading,
  highlightWorkload = null,
}: {
  tradeEnv: TradeEnv
  namespace: string
  canOperate: boolean
  workloads: ClusterWorkload[]
  workloadsLoading: boolean
  /** Activity / in-flight actuation workload to emphasize (account-sync | daemon). */
  highlightWorkload?: string | null
}) {
  const { events } = useActivityFeed()

  const applyingRestart = useMemo(() => {
    const prefix = `actuation:daemon-restart:${namespace}/`
    return events.find(e => e.id.startsWith(prefix) && isActivityInFlight(e)) ?? null
  }, [events, namespace])

  const daemon = useMemo(() => findWorkload(workloads, 'daemon'), [workloads])
  const accountSync = useMemo(() => findWorkload(workloads, 'account-sync'), [workloads])
  const daemonReplicas = parseDesiredReplicas(daemon?.ready)
  const syncReplicas = parseDesiredReplicas(accountSync?.ready)
  const daemonRunning = daemonReplicas == null ? true : daemonReplicas > 0
  const syncRunning = syncReplicas != null && syncReplicas > 0

  return (
    <OpsSection
      variant="flat"
      title="Trade daemon operate"
      bodyPadding="none"
      overflow="hidden"
      description={`${namespace} · co-scaled pair (daemon ↔ account-sync). Daemon Start blocked by D10 until Owner unlock.`}
    >
      <div className="flex flex-col gap-2 px-2.5 py-2">
        {!canOperate && (
          <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
            Authenticate with operator token to scale or restart workloads.
          </p>
        )}

        <div
          className={cn(
            'flex flex-wrap items-center gap-2 rounded-md px-1.5 py-1 -mx-1.5',
            highlightWorkload === 'account-sync' &&
              'bg-[color-mix(in_oklab,var(--color-info,#38bdf8)_12%,transparent)] ring-1 ring-[color-mix(in_oklab,var(--color-info,#38bdf8)_50%,var(--border))]',
          )}
        >
          <DenseTag variant="neutral" className="shrink-0 text-[10px] uppercase tracking-wide">
            account-sync
          </DenseTag>
          <WorkloadRolloutMeta workload={accountSync} loading={workloadsLoading} />
          {highlightWorkload === 'account-sync' && (
            <DenseTag variant="info" className="text-[9px] uppercase tracking-wide">
              Actuation target
            </DenseTag>
          )}
          <RequestScaleDeployment
            namespace={namespace}
            name="account-sync"
            replicas={1}
            label="Request start"
            disabled={!canOperate || syncRunning}
          />
          <RequestScaleDeployment
            namespace={namespace}
            name="account-sync"
            replicas={0}
            label="Request stop"
            disabled={!canOperate || !syncRunning}
          />
          <RequestRestartDeployment
            namespace={namespace}
            name="account-sync"
            disabled={!canOperate}
          />
        </div>

        <div
          className={cn(
            'flex flex-wrap items-center gap-2 rounded-md px-1.5 py-1 -mx-1.5',
            highlightWorkload === 'daemon' &&
              'bg-[color-mix(in_oklab,var(--color-info,#38bdf8)_12%,transparent)] ring-1 ring-[color-mix(in_oklab,var(--color-info,#38bdf8)_50%,var(--border))]',
          )}
        >
          <DenseTag variant="neutral" className="shrink-0 text-[10px] uppercase tracking-wide">
            daemon
          </DenseTag>
          {highlightWorkload === 'daemon' && (
            <DenseTag variant="info" className="text-[9px] uppercase tracking-wide">
              Actuation target
            </DenseTag>
          )}
          <WorkloadRolloutMeta workload={daemon} loading={workloadsLoading} />
          <Button
            size="sm"
            variant="outline"
            disabled
            title="Trading execution is BLOCKED (D10). Daemon scale-up requires Owner unlock."
          >
            Start
          </Button>
          <RequestScaleDeployment
            namespace={namespace}
            name="daemon"
            replicas={0}
            label="Request stop"
            disabled={!canOperate || daemonReplicas === 0}
          />
          <RequestRestartDeployment
            namespace={namespace}
            name="daemon"
            disabled={!canOperate || !daemonRunning}
          />
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            D10 · {tradeEnv.toUpperCase()} Start disabled
          </span>
        </div>

        {applyingRestart != null && (
          <OpsFeedback variant="info" title="Rollout in progress">
            {applyingRestart.detail ?? applyingRestart.title}
          </OpsFeedback>
        )}
      </div>
    </OpsSection>
  )
}
