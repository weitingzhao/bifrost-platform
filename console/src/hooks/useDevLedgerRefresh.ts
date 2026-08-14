import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchDataFreshness } from '@/api/cluster'
import type { DataFreshnessDatabase } from '@/api/clusterTypes'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { DATA_LAYER_CLONE_SCOPE } from '@/lib/agent/agentScopes'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { buildDataLayerCloneOperatorPrompt } from '@/lib/agent/dataLayerClonePrompt'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { resolveDevLedgerSignal } from '@/lib/task-mode/devLedgerSignal'

function pickDevDb(rows: DataFreshnessDatabase[] | undefined): DataFreshnessDatabase | undefined {
  return rows?.find(d => d.name === 'bifrost_dev' || d.environment === 'dev')
}

export function useDevLedgerRefresh({
  canOperate,
  ambientJobId,
  ambientJobStatus,
  onStartAgentJob,
  runnerHealthy,
  enabled = true,
}: Pick<AmbientAgentShellProps, 'ambientJobId' | 'ambientJobStatus' | 'onStartAgentJob'> & {
  canOperate: boolean
  runnerHealthy: boolean
  enabled?: boolean
}) {
  const [confirmOpen, setConfirmOpen] = useState(false)

  const freshnessQ = useQuery({
    queryKey: ['cluster', 'data-freshness'],
    queryFn: fetchDataFreshness,
    refetchInterval: 60_000,
    enabled,
  })

  const devDb = pickDevDb(freshnessQ.data?.databases)
  const lastCloneAt = devDb?.last_clone_at ?? freshnessQ.data?.last_clone_at ?? null
  const lagDays = devDb?.lag_vs_prod_days ?? null
  const verdict = devDb?.verdict ?? null
  const signal = resolveDevLedgerSignal({ lastCloneAt, lagDays, verdict })

  const task = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    ambientJobStatus,
    onStartAgentJob,
    scope: DATA_LAYER_CLONE_SCOPE,
    label: scopeToLabel(DATA_LAYER_CLONE_SCOPE),
    buildRequest: () => ({
      prompt: buildDataLayerCloneOperatorPrompt({
        lastCloneAt,
        lagDays,
        verdict,
      }),
    }),
  })

  useEffect(() => {
    if (!confirmOpen) return
    if (task.isActive || task.error != null) setConfirmOpen(false)
  }, [confirmOpen, task.isActive, task.error])

  const disabledReason = !runnerHealthy
    ? 'Remediation runner not healthy — check Engineer · runners-ha'
    : (task.disabledReason ?? null)

  const confirmMessage = useMemo(() => {
    const lag = lagDays != null && Number.isFinite(lagDays) ? `${lagDays}d vs prod` : 'lag unknown'
    return [
      `Overwrite bifrost_dev with a Full clone of bifrost_prod, then bounce bifrost-dev Trade APIs (api-*).`,
      `Last clone: ${signal.lastCloneLabel}. ${lag}.`,
      `bifrost_stg and bifrost_prod are not touched. Live quotes stay on redis-ib (no redis-live dump). D10 remains blocked.`,
    ].join(' ')
  }, [signal.lastCloneLabel, lagDays])

  return {
    lastCloneAt,
    lastCloneLabel: signal.lastCloneLabel,
    lagDays,
    verdict,
    lamp: freshnessQ.isLoading ? 'unknown' : signal.lamp,
    blocking: freshnessQ.isLoading || freshnessQ.isError ? false : signal.blocking,
    chipLabel: signal.chipLabel,
    freshnessLoading: freshnessQ.isLoading,
    confirmOpen,
    confirmMessage,
    requestConfirm: () => setConfirmOpen(true),
    cancelConfirm: () => setConfirmOpen(false),
    confirm: () => task.trigger(),
    isPending: task.isPending,
    isActive: task.isActive,
    disabled: task.disabled || !runnerHealthy,
    disabledReason,
    error: task.error,
  }
}

export type DevLedgerRefresh = ReturnType<typeof useDevLedgerRefresh>
