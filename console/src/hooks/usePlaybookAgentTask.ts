import { useMutation, useQueryClient } from '@tanstack/react-query'
import { startRemediation } from '@/api/platform'
import type { StartRemediationRequest } from '@/api/types'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  ambientAgentBlockedReason,
  isAmbientAgentActive,
  type AmbientAgentShellProps,
} from '@/lib/agent/ambientAgent'

type UsePlaybookAgentTaskOptions = AmbientAgentShellProps & {
  canOperate: boolean
  scope: string
  buildRequest: () => StartRemediationRequest | Promise<StartRemediationRequest>
}

/** One-click remediation task launcher (Mission Board, Cluster triage, Defects Fix). */
export function usePlaybookAgentTask({
  canOperate,
  ambientJobId,
  onStartAgentJob,
  scope,
  buildRequest,
}: UsePlaybookAgentTaskOptions) {
  const qc = useQueryClient()
  const label = scopeToLabel(scope)

  const mutation = useMutation({
    mutationFn: async () => {
      const body = await buildRequest()
      return startRemediation({ scope, ...body })
    },
    onSuccess: job => {
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      onStartAgentJob?.({ id: job.id, scope, label })
    },
  })

  const disabledReason = ambientAgentBlockedReason(canOperate, ambientJobId, onStartAgentJob)

  return {
    trigger: () => mutation.mutate(),
    isPending: mutation.isPending,
    isActive: isAmbientAgentActive(ambientJobId),
    disabled: disabledReason != null || mutation.isPending,
    disabledReason,
    error: mutation.error as Error | null,
    label,
  }
}
