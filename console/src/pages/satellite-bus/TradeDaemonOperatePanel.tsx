import { useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button, ConfirmDialog, DenseTag, cn } from '@bifrost/ui'
import { rolloutRestartDeployment, scaleDeployment } from '@/api/clusterActuation'
import type { ClusterWorkload } from '@/api/clusterTypes'
import { isActivityInFlight } from '@/lib/activity/activityPageFocus'
import {
  upsertActivity,
  updateActivityPhase,
  useActivityFeed,
} from '@/lib/activity/activityStore'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import { startRestartActuationSettle } from '@/lib/activity/restartActuationSettle'
import { formatWorkloadRollout } from '@/lib/cluster/workloadRollout'
import type { TradeEnv } from '@/pages/satellite-bus/useSatelliteBusQueries'

type ConfirmTarget = {
  title: string
  message: string
  confirmLabel: string
  action: () => void
}

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
  const qc = useQueryClient()
  const { events } = useActivityFeed()
  const [confirm, setConfirm] = useState<ConfirmTarget | null>(null)
  const [feedback, setFeedback] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null)

  const applyingRestart = useMemo(() => {
    const prefix = `actuation:daemon-restart:${namespace}/`
    return events.find(e => e.id.startsWith(prefix) && isActivityInFlight(e)) ?? null
  }, [events, namespace])

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['satellite'] })
  }

  const scaleMutation = useMutation({
    mutationFn: scaleDeployment,
    onMutate: vars => {
      upsertActivity({
        id: `actuation:daemon-scale:${vars.namespace}/${vars.name}`,
        kind: 'actuation',
        phase: 'requested',
        title: `Scale ${vars.name} → ${vars.replicas}`,
        target: `${vars.namespace}/${vars.name}`,
        linkTo: 'satellite-bus',
        bumpTs: true,
      })
    },
    onSuccess: (data, vars) => {
      updateActivityPhase(`actuation:daemon-scale:${vars.namespace}/${vars.name}`, 'settled', {
        settledOutcome: 'resolved',
        detail: data.message,
      })
      setFeedback({ kind: 'ok', text: data.message })
      setConfirm(null)
      invalidate()
    },
    onError: (err: Error, vars) => {
      updateActivityPhase(`actuation:daemon-scale:${vars.namespace}/${vars.name}`, 'failed', {
        settledOutcome: 'error',
        detail: err.message,
      })
      setFeedback({ kind: 'err', text: err.message })
      setConfirm(null)
    },
  })

  const restartMutation = useMutation({
    mutationFn: rolloutRestartDeployment,
    onMutate: vars => {
      upsertActivity({
        id: `actuation:daemon-restart:${vars.namespace}/${vars.name}`,
        kind: 'actuation',
        phase: 'requested',
        title: `Restart ${vars.name}`,
        target: `${vars.namespace}/${vars.name}`,
        linkTo: 'satellite-bus',
        bumpTs: true,
      })
    },
    onSuccess: (data, vars) => {
      const activityId = `actuation:daemon-restart:${vars.namespace}/${vars.name}`
      const wl = findWorkload(workloads, vars.name)
      startRestartActuationSettle({
        activityId,
        queryClient: qc,
        namespace: vars.namespace,
        name: vars.name,
        baselineReady: wl?.ready ?? null,
        baselineGeneration: wl?.generation ?? null,
        apiMessage: data.message,
      })
      setFeedback({ kind: 'ok', text: data.message })
      setConfirm(null)
      invalidate()
    },
    onError: (err: Error, vars) => {
      updateActivityPhase(`actuation:daemon-restart:${vars.namespace}/${vars.name}`, 'failed', {
        settledOutcome: 'error',
        detail: err.message,
      })
      setFeedback({ kind: 'err', text: err.message })
      setConfirm(null)
    },
  })

  const pending = scaleMutation.isPending || restartMutation.isPending
  const daemon = useMemo(() => findWorkload(workloads, 'daemon'), [workloads])
  const accountSync = useMemo(() => findWorkload(workloads, 'account-sync'), [workloads])
  const daemonReplicas = parseDesiredReplicas(daemon?.ready)
  const syncReplicas = parseDesiredReplicas(accountSync?.ready)
  const daemonRunning = daemonReplicas == null ? true : daemonReplicas > 0
  const syncRunning = syncReplicas != null && syncReplicas > 0

  const ask = (next: ConfirmTarget) => {
    setFeedback(null)
    setConfirm(next)
  }

  const scale = (name: string, replicas: number, title: string, message: string, confirmLabel: string) => {
    ask({
      title,
      message,
      confirmLabel,
      action: () =>
        scaleMutation.mutate({
          namespace,
          kind: 'Deployment',
          name,
          replicas,
        }),
    })
  }

  const restart = (name: string) => {
    ask({
      title: `Restart ${name}`,
      message: `Request Kubernetes rollout restart for ${namespace}/${name}. Co-scaled pair: daemon ↔ account-sync.`,
      confirmLabel: 'Restart',
      action: () =>
        restartMutation.mutate({
          namespace,
          kind: 'Deployment',
          name,
        }),
    })
  }

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
          <Button
            size="sm"
            variant="outline"
            disabled={!canOperate || pending || syncRunning}
            onClick={() =>
              scale(
                'account-sync',
                1,
                'Start account-sync',
                `Scale ${namespace}/account-sync to 1 replica.`,
                'Start',
              )
            }
          >
            Start
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={!canOperate || pending || !syncRunning}
            onClick={() =>
              scale(
                'account-sync',
                0,
                'Stop account-sync',
                `Scale ${namespace}/account-sync to 0 replicas.`,
                'Stop',
              )
            }
          >
            Stop
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={!canOperate || pending}
            onClick={() => restart('account-sync')}
          >
            Restart
          </Button>
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
          <Button
            size="sm"
            variant="outline"
            disabled={!canOperate || pending || daemonReplicas === 0}
            onClick={() =>
              scale(
                'daemon',
                0,
                'Stop daemon',
                `Scale ${namespace}/daemon to 0 replicas. Co-scale tip: stop account-sync separately if needed.`,
                'Stop',
              )
            }
          >
            Stop
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={!canOperate || pending || !daemonRunning}
            title={
              daemonReplicas === 0
                ? 'Daemon replicas are 0 — Restart unavailable (D10 blocks scale-up)'
                : undefined
            }
            onClick={() => restart('daemon')}
          >
            Restart
          </Button>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            D10 · {tradeEnv.toUpperCase()} Start disabled
          </span>
        </div>

        {applyingRestart != null && (
          <OpsFeedback variant="info" title="Rollout in progress">
            {applyingRestart.detail ?? applyingRestart.title}
          </OpsFeedback>
        )}
        {applyingRestart == null && feedback?.kind === 'ok' && (
          <OpsFeedback variant="success" title="Actuation ok">
            {feedback.text}
          </OpsFeedback>
        )}
        {feedback?.kind === 'err' && (
          <OpsFeedback variant="error" title="Actuation failed">
            {feedback.text}
          </OpsFeedback>
        )}
      </div>

      <ConfirmDialog
        open={confirm != null}
        title={confirm?.title ?? ''}
        message={confirm?.message ?? ''}
        confirmLabel={confirm?.confirmLabel}
        confirming={pending}
        onConfirm={() => confirm?.action()}
        onCancel={() => setConfirm(null)}
      />
    </OpsSection>
  )
}
