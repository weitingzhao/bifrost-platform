import { useCallback, useMemo, useState } from 'react'
import type { ClusterObservabilityResponse, ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import {
  useDevProgramInstance,
  type UseDevProgramInstanceResult,
} from '@/hooks/useDevProgramInstance'
import { useInlineBriefingPack, type InlineBriefingPackResult } from '@/hooks/useInlineBriefingPack'
import { isBriefingOpened } from '@/lib/task-mode/briefingOpenedFlag'
import type { TaskModeDef } from '@/lib/task-mode/types'
import { phaseJoinKey } from '@/lib/briefing/phaseJoinKey'

/**
 * Dev-loop wiring extracted from TaskControlCenter — Delivery Board program
 * instance and inline Briefing pack. Playbook phase-done uses Delivery program
 * phases (Cursor IDE Agent), not a Console SDK runtime pointer.
 */
export type DevModeControllerResult = {
  devProgram: UseDevProgramInstanceResult
  resolvedProgramId?: string
  inlineBriefingPack: InlineBriefingPackResult
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

  const [briefingOpenedTick, setBriefingOpenedTick] = useState(0)
  const handleBriefingOpened = useCallback(() => {
    setBriefingOpenedTick(t => t + 1)
  }, [])

  const briefingOpened = useMemo(() => {
    void briefingOpenedTick
    if (!isDevLoop) return false
    return isBriefingOpened(mode.id, resolvedProgramId)
  }, [isDevLoop, mode.id, resolvedProgramId, briefingOpenedTick])

  const deliveryPhases = devProgram.programDetail?.phases

  const devAgentPhaseDone = useCallback(
    (phaseId: string): boolean => {
      const phases = deliveryPhases ?? []
      if (phases.length === 0) return false
      const key = phaseJoinKey(phaseId)
      const exact = phases.find(p => phaseJoinKey(p.id) === key)
      if (exact != null) {
        return exact.status === 'done' || exact.signed_off === true
      }
      if (phaseId === 'implement') {
        const impl = phases.find(
          p =>
            p.id === 'implement' ||
            p.id.includes('implement') ||
            /implement/i.test(p.title ?? ''),
        )
        return impl != null && (impl.status === 'done' || impl.signed_off === true)
      }
      if (phaseId === 'pre-push') {
        const prePush = phases.find(
          p =>
            p.id === 'pre-push' ||
            p.id.includes('pre-push') ||
            p.id.includes('verify') ||
            /pre.?push/i.test(p.title ?? ''),
        )
        if (prePush != null) return prePush.status === 'done' || prePush.signed_off === true
        return phases.every(p => p.status === 'done' || p.signed_off === true)
      }
      return false
    },
    [deliveryPhases],
  )

  return {
    devProgram,
    resolvedProgramId,
    inlineBriefingPack,
    briefingOpened,
    handleBriefingOpened,
    devAgentPhaseDone,
  }
}
