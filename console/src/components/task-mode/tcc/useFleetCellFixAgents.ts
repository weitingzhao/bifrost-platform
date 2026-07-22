import { useEffect, type MutableRefObject } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCluster, fetchClusterServiceReadiness } from '@/api/cluster'
import { fetchSupplyChain } from '@/api/delivery'
import { fetchStgSmoke } from '@/api/promote'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import {
  buildClusterPackBody,
  buildDispatchedFixPrompt,
} from '@/lib/agent/readinessFixDispatch'
import {
  buildFleetCellFixPrompt,
  cellAllowsAgentFix,
  pickFleetFixCell,
  resolveCellFixScope,
} from '@/lib/control-room/fleetCellFix'
import { recordChecklistRunTouch } from '@/lib/control-room/dailyOpsChecklistCoverage'
import type { FleetCell, FleetSnapshot } from '@/lib/control-room/fleetSnapshot'

export function useFleetCellFixAgents({
  isDailyOps,
  canOperate,
  ambientJobId,
  onStartAgentJob,
  onNavigate,
  fleet,
  setFleetFixCell,
  fleetFixCellRef,
  dailyOpsTargetCell,
  dailyOpsFixScope,
  clusterForFixQ,
  serviceReadinessForFixQ,
  dailyOpsFixStartedRef,
  setAgentJustSucceeded,
  otherAgentPending,
}: Pick<AmbientAgentShellProps, 'ambientJobId' | 'onStartAgentJob'> & {
  isDailyOps: boolean
  canOperate: boolean
  fleet: FleetSnapshot
  setFleetFixCell: (cell: FleetCell | null) => void
  fleetFixCellRef: MutableRefObject<FleetCell | null>
  dailyOpsTargetCell: FleetCell | null
  dailyOpsFixScope: string
  clusterForFixQ: ReturnType<typeof useQuery<Awaited<ReturnType<typeof fetchCluster>>>>
  serviceReadinessForFixQ: ReturnType<
    typeof useQuery<Awaited<ReturnType<typeof fetchClusterServiceReadiness>>>
  >
  dailyOpsFixStartedRef: MutableRefObject<boolean>
  setAgentJustSucceeded: (value: boolean) => void
  otherAgentPending: boolean
  onNavigate: (tabId: string) => void
}) {
  const aiDailyOpsFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: dailyOpsFixScope,
    label: scopeToLabel(dailyOpsFixScope),
    buildRequest: async () => {
      const cell =
        fleetFixCellRef.current ??
        dailyOpsTargetCell ??
        pickFleetFixCell(fleet)
      if (cell == null || !cellAllowsAgentFix(cell)) {
        throw new Error('Fleet is clear or selected cell is not Agent-Fixable')
      }
      const scope = resolveCellFixScope(cell) ?? PROD_ENV_FIX_SCOPE
      const cellPrompt = buildFleetCellFixPrompt(cell, fleet)
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const pack = buildClusterPackBody({ cluster, serviceReadiness })
      const [supply, smoke] = await Promise.all([fetchSupplyChain(), fetchStgSmoke()])
      return {
        prompt: buildDispatchedFixPrompt({
          scope,
          signals: [
            {
              label: `${cell.role} · ${cell.env ?? 'span'}`,
              signal: cell.signal === 'unavailable' ? 'unknown' : cell.signal,
              detail: cell.detail,
              fixScope: scope,
            },
          ],
          clusterFallbackPrompt: cellPrompt,
          extras: { supply, stgSmoke: smoke },
        }),
        ...(scope === PROD_ENV_FIX_SCOPE ? pack : {}),
      }
    },
  })

  const handleFleetCellFix = (cell: FleetCell) => {
    fleetFixCellRef.current = cell
    setFleetFixCell(cell)
    dailyOpsFixStartedRef.current = true
    setAgentJustSucceeded(false)
    recordChecklistRunTouch(cell)
    aiDailyOpsFix.trigger()
  }

  const handleFleetPrimaryCta = () => {
    const cta = fleet.verdict.primaryCta
    if (cta.kind === 'navigate' && cta.tabId != null) {
      onNavigate(cta.tabId)
      return
    }
    if (cta.kind === 'agent-fix') {
      const cell =
        (cta.cellKey != null ? fleet.cells.find(c => c.key === cta.cellKey) : null) ??
        pickFleetFixCell(fleet)
      if (cell != null) handleFleetCellFix(cell)
    }
  }

  useEffect(() => {
    if (fleet.fleetClear) {
      setAgentJustSucceeded(false)
      dailyOpsFixStartedRef.current = false
    }
  }, [fleet.fleetClear, dailyOpsFixStartedRef, setAgentJustSucceeded])

  const dailyOpsAgentPending =
    isDailyOps &&
    (aiDailyOpsFix.isPending ||
      otherAgentPending ||
      (ambientJobId != null && dailyOpsFixStartedRef.current))

  return {
    aiDailyOpsFix,
    handleFleetCellFix,
    handleFleetPrimaryCta,
    dailyOpsAgentPending,
  }
}

export type FleetCellFixAgents = ReturnType<typeof useFleetCellFixAgents>
