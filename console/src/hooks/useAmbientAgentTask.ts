import { useMutation, useQueryClient } from '@tanstack/react-query'
import { startRemediation } from '@/api/remediation'
import type { StartRemediationRequest } from '@/api/remediationTypes'
import {
  ambientAgentBlockedReason,
  isAmbientAgentActive,
  type AmbientAgentShellProps,
} from '@/lib/agent/ambientAgent'

type UseAmbientAgentTaskOptions = AmbientAgentShellProps & {
  canOperate: boolean
  scope: string
  label: string
  buildRequest: () => StartRemediationRequest | Promise<StartRemediationRequest>
}

export function useAmbientAgentTask({
  canOperate,
  ambientJobId,
  ambientJobStatus,
  onStartAgentJob,
  scope,
  label,
  buildRequest,
}: UseAmbientAgentTaskOptions) {
  const qc = useQueryClient()

  const mutation = useMutation({
    mutationFn: async () => {
      const body = await buildRequest()
      return startRemediation({ scope, ...body })
    },
    onSuccess: job => {
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      onStartAgentJob?.({ id: job.id, scope, label, status: 'running' })
    },
  })

  const disabledReason = ambientAgentBlockedReason(
    canOperate,
    ambientJobId,
    onStartAgentJob,
    ambientJobStatus,
  )
  const isActive = isAmbientAgentActive(ambientJobId, ambientJobStatus)

  return {
    trigger: () => mutation.mutate(),
    isPending: mutation.isPending,
    isActive,
    disabled: disabledReason != null || mutation.isPending,
    disabledReason,
    error: mutation.error as Error | null,
  }
}
