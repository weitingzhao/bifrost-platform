import { useCallback, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchDevAgentStatus } from '@/api/devAgent'
import type { ClusterObservabilityResponse, ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { DevTaskStrips } from '@/components/task-mode/DevTaskStrips'
import {
  useDevProgramInstance,
  type UseDevProgramInstanceResult,
} from '@/hooks/useDevProgramInstance'
import { useInlineBriefingPack, type InlineBriefingPackResult } from '@/hooks/useInlineBriefingPack'
import { isBriefingOpened } from '@/lib/task-mode/briefingOpenedFlag'
import type { TaskModeDef } from '@/lib/task-mode/types'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'

/**
 * Dev-loop wiring extracted from TaskControlCenter — Delivery Board program
 * instance, inline Briefing pack, and Dev Agent status. Keeps the "which
 * playbook phase is the dev agent currently on" logic in one place so
 * TaskControlCenter only has to render the result.
 */
export type DevModeControllerResult = {
  devProgram: UseDevProgramInstanceResult
  resolvedProgramId?: string
  inlineBriefingPack: InlineBriefingPackResult
  devAgentQ: ReturnType<typeof useQuery<Awaited<ReturnType<typeof fetchDevAgentStatus>>>>
  briefingOpened: boolean
  handleBriefingOpened: () => void
  devAgentPhaseDone: (phaseId: string) => boolean
}

export function useDevModeController({
  mode,
  isDevLoop,
  context,
  matrices,
  clusterSummary,
  clusterObservability,
  platformHealthy,
}: {
  mode: TaskModeDef
  isDevLoop: boolean
  context?: OpsContextResponse
  matrices?: MatrixResponse[]
  clusterSummary?: ClusterSummary
  clusterObservability?: ClusterObservabilityResponse
  platformHealthy?: boolean
}): DevModeControllerResult {
  const devProgram = useDevProgramInstance(mode)
  const resolvedProgramId = devProgram.programId ?? mode.dev?.programId

  const inlineBriefingPack = useInlineBriefingPack({
    mode,
    context,
    matrices,
    clusterSummary,
    clusterObservability,
    platformHealthy,
    programId: resolvedProgramId,
    enabled: isDevLoop,
  })

  const devAgentQ = useQuery({
    queryKey: ['dev-agent', 'status'],
    queryFn: fetchDevAgentStatus,
    refetchInterval: 5000,
    enabled: isDevLoop,
  })

  const [briefingOpenedTick, setBriefingOpenedTick] = useState(0)
  const handleBriefingOpened = useCallback(() => {
    setBriefingOpenedTick(t => t + 1)
  }, [])

  const briefingOpened = useMemo(() => {
    void briefingOpenedTick
    if (!isDevLoop) return false
    return isBriefingOpened(mode.id, resolvedProgramId)
  }, [isDevLoop, mode.id, resolvedProgramId, briefingOpenedTick])

  const devAgentPhaseDone = useCallback(
    (phaseId: string): boolean => {
      const agentPhases = devAgentQ.data?.phases ?? []
      if (agentPhases.length === 0) return false
      const exact = agentPhases.find(p => p.id === phaseId)
      if (exact != null) return exact.status === 'done'
      if (phaseId === 'implement') {
        const impl = agentPhases.find(
          p =>
            p.id === 'implement' ||
            p.id.includes('implement') ||
            /implement/i.test(p.title ?? ''),
        )
        return impl != null && impl.status === 'done'
      }
      if (phaseId === 'pre-push') {
        const prePush = agentPhases.find(
          p =>
            p.id === 'pre-push' ||
            p.id.includes('pre-push') ||
            p.id.includes('verify') ||
            /pre.?push/i.test(p.title ?? ''),
        )
        if (prePush != null) return prePush.status === 'done'
        return agentPhases.length > 0 && agentPhases.every(p => p.status === 'done')
      }
      return false
    },
    [devAgentQ.data?.phases],
  )

  return {
    devProgram,
    resolvedProgramId,
    inlineBriefingPack,
    devAgentQ,
    briefingOpened,
    handleBriefingOpened,
    devAgentPhaseDone,
  }
}

/** Renders the Dev-loop strips block (Delivery Board program + inline Briefing + Dev Agent). */
export function DevModeStrips({
  mode,
  canOperate,
  devProgram,
  resolvedProgramId,
  onNavigate,
  inlineBriefingPack,
  onOpenFullBriefing,
  onBriefingOpened,
  devAgentQ,
}: {
  mode: TaskModeDef
  canOperate?: boolean
  devProgram: UseDevProgramInstanceResult
  resolvedProgramId?: string
  onNavigate: (tabId: string) => void
  inlineBriefingPack: InlineBriefingPackResult
  onOpenFullBriefing?: (opts?: BriefingUrlState) => void
  onBriefingOpened?: () => void
  devAgentQ: DevModeControllerResult['devAgentQ']
}) {
  return (
    <DevTaskStrips
      mode={mode}
      canOperate={canOperate}
      programDetail={devProgram.programDetail}
      programLoading={devProgram.programLoading}
      programError={devProgram.programError}
      resolvedProgramId={resolvedProgramId}
      createPending={devProgram.createPending}
      hasActiveSession={devProgram.hasActiveSession}
      activeLane={devProgram.activeLane}
      canCreateProgram={devProgram.canCreateProgram}
      onCreateProgram={devProgram.ensureProgram}
      onCreateNewInstance={() => devProgram.createNewInstance()}
      onNavigate={onNavigate}
      inlineBriefingPack={inlineBriefingPack}
      onOpenFullBriefing={onOpenFullBriefing}
      onBriefingOpened={onBriefingOpened}
      devAgentStatus={devAgentQ.data}
      devAgentLoading={devAgentQ.isLoading}
    />
  )
}
