import { useMutation } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { cancelRemediationJob } from '@/api/remediation'
import type { RemediationJob } from '@/api/remediationTypes'
import { CLUSTER_ISSUES_FULL_AUTO_SCOPE } from '@/lib/agent/agentScopes'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { ClusterMutationActuation, ClusterPageMutationsInput } from './clusterMutationTypes'

export function useClusterRemediationMutations(
  actuation: ClusterMutationActuation,
  input: Pick<
    ClusterPageMutationsInput,
    | 'queries'
    | 'onOpenAgentDesk'
    | 'onExpandAgentDock'
    | 'onSelectAgentJob'
  >,
) {
  const {
    queries,
    onOpenAgentDesk,
    onExpandAgentDock,
    onSelectAgentJob,
  } = input
  const { qc } = actuation
  const [remediationPanelOpen, setRemediationPanelOpen] = useState(false)
  const [remediationJobId, setRemediationJobId] = useState<string | null>(null)
  const [remediationJob, setRemediationJob] = useState<RemediationJob | null>(null)

  const remediationCancelMutation = useMutation({
    mutationFn: cancelRemediationJob,
    onSuccess: job => {
      setRemediationJob(job)
    },
    onError: (err: Error) => actuation.setActionError(err.message),
  })

  /** Track ambient dock job on this page — do not open the page RemediationPanel (dock owns UI). */
  const followAmbientRemediationJob = useCallback(
    (jobId: string) => {
      const job =
        queries.remediationJobsQuery.data?.jobs?.find(j => j.id === jobId) ??
        (remediationJobId === jobId ? remediationJob : null)
      if (job != null) setRemediationJob(job)
      setRemediationJobId(jobId)
      setRemediationPanelOpen(false)
      actuation.setActionError(null)
    },
    [queries.remediationJobsQuery.data?.jobs, remediationJob, remediationJobId, actuation],
  )

  const handleOpenRemediationSession = useCallback(
    (jobId: string) => {
      const job =
        queries.remediationJobsQuery.data?.jobs?.find(j => j.id === jobId) ??
        (remediationJobId === jobId ? remediationJob : null)
      if (job != null) setRemediationJob(job)
      setRemediationJobId(jobId)
      setRemediationPanelOpen(false)
      actuation.setActionError(null)

      // Prefer Operator Dock over page drawer / Agent Desk tab.
      if (onSelectAgentJob != null && job != null) {
        const scope = job.scope ?? CLUSTER_ISSUES_FULL_AUTO_SCOPE
        const status =
          job.status === 'done' || job.status === 'failed' || job.status === 'cancelled'
            ? job.status
            : 'running'
        onSelectAgentJob({
          id: job.id,
          scope,
          label: scopeToLabel(scope),
          status,
        })
        return
      }
      if (onExpandAgentDock != null) {
        onExpandAgentDock()
        return
      }
      if (onOpenAgentDesk != null) {
        onOpenAgentDesk(jobId)
        return
      }
      setRemediationPanelOpen(true)
    },
    [
      onSelectAgentJob,
      onExpandAgentDock,
      onOpenAgentDesk,
      remediationJob,
      remediationJobId,
      queries.remediationJobsQuery.data?.jobs,
      actuation,
    ],
  )

  const handleRemediationComplete = useCallback(
    (job: RemediationJob) => {
      setRemediationJob(job)
      void qc.invalidateQueries({ queryKey: ['cluster'] })
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      window.setTimeout(() => {
        void qc.invalidateQueries({ queryKey: ['cluster'] })
      }, 3_000)
      if (job.status === 'done') {
        actuation.setActionError(null)
        actuation.handleActuationSuccess(job.summary ?? 'Remediation completed — cluster data refreshed')
      }
    },
    [actuation, qc],
  )

  return {
    remediationPanelOpen,
    setRemediationPanelOpen,
    remediationJobId,
    remediationJob,
    remediationCancelMutation,
    followAmbientRemediationJob,
    handleOpenRemediationSession,
    handleRemediationComplete,
  }
}

export type ClusterRemediationMutations = ReturnType<typeof useClusterRemediationMutations>
