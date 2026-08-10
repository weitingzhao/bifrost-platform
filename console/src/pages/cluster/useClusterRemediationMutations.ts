import { useMutation } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { startRemediation, cancelRemediationJob } from '@/api/remediation'
import type { RemediationJob } from '@/api/remediationTypes'
import { collectClusterIssues } from '@/lib/cluster/collectClusterIssues'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { buildClusterLlmContext } from '@/lib/cluster/buildClusterLlmContext'
import type { ClusterMutationActuation, ClusterPageMutationsInput } from './clusterMutationTypes'

export function useClusterRemediationMutations(
  actuation: ClusterMutationActuation,
  input: Pick<
    ClusterPageMutationsInput,
    | 'clusterSummary'
    | 'serviceReadiness'
    | 'governance'
    | 'postgresStatus'
    | 'queries'
    | 'selectedNs'
    | 'onOpenAgentDesk'
    | 'onStartAgentJob'
  >,
) {
  const {
    clusterSummary,
    serviceReadiness,
    governance,
    postgresStatus,
    queries,
    selectedNs,
    onOpenAgentDesk,
    onStartAgentJob,
  } = input
  const { handleActuationError, qc } = actuation
  const [remediationPanelOpen, setRemediationPanelOpen] = useState(false)
  const [remediationJobId, setRemediationJobId] = useState<string | null>(null)
  const [remediationJob, setRemediationJob] = useState<RemediationJob | null>(null)

  const playbookFixMutation = useMutation({
    mutationFn: ({ scope, prompt }: { scope: string; prompt: string }) =>
      startRemediation({ scope, prompt }),
    onSuccess: (job, vars) => {
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      actuation.setActionError(null)
      if (onStartAgentJob != null) {
        onStartAgentJob({ id: job.id, scope: vars.scope, label: scopeToLabel(vars.scope) })
        return
      }
      if (onOpenAgentDesk != null) {
        onOpenAgentDesk(job.id)
        return
      }
      setRemediationJob(job)
      setRemediationJobId(job.id)
      setRemediationPanelOpen(true)
    },
    onError: handleActuationError,
  })

  const remediationStartMutation = useMutation({
    mutationFn: startRemediation,
    onSuccess: job => {
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      if (onOpenAgentDesk != null) {
        onOpenAgentDesk(job.id)
        actuation.setActionError(null)
        return
      }
      setRemediationJob(job)
      setRemediationJobId(job.id)
      setRemediationPanelOpen(true)
      actuation.setActionError(null)
    },
    onError: (err: Error) => actuation.setActionError(err.message),
  })

  const remediationCancelMutation = useMutation({
    mutationFn: cancelRemediationJob,
    onSuccess: job => {
      setRemediationJob(job)
    },
    onError: (err: Error) => actuation.setActionError(err.message),
  })

  const handleAutoRemediate = useCallback(() => {
    if (clusterSummary == null) return
    remediationStartMutation.mutate({
      scope: 'cluster_issues_full_auto',
      cluster_summary: clusterSummary,
      service_readiness: serviceReadiness,
      governance,
      issues: collectClusterIssues({
        summary: clusterSummary,
        serviceReadiness,
        postgresStatus,
      }),
      prompt: buildClusterLlmContext({
        summary: clusterSummary,
        nodes: queries.nodesQuery.data?.nodes,
        governance,
        serviceReadiness,
        metrics: queries.metricsQuery.data,
        namespaces: queries.namespacesQuery.data?.namespaces,
        placement: queries.placementQuery.data,
        observability: queries.observabilityQuery.data,
        selectedNamespace: selectedNs,
        workloads: queries.workloadsQuery.data?.workloads,
      }),
    })
  }, [
    clusterSummary,
    governance,
    postgresStatus,
    queries.metricsQuery.data,
    queries.namespacesQuery.data?.namespaces,
    queries.nodesQuery.data?.nodes,
    queries.observabilityQuery.data,
    queries.placementQuery.data,
    queries.workloadsQuery.data?.workloads,
    remediationStartMutation,
    selectedNs,
    serviceReadiness,
  ])

  const followAmbientRemediationJob = useCallback(
    (jobId: string) => {
      const job =
        queries.remediationJobsQuery.data?.jobs?.find(j => j.id === jobId) ??
        (remediationJobId === jobId ? remediationJob : null)
      if (job != null) setRemediationJob(job)
      setRemediationJobId(jobId)
      setRemediationPanelOpen(true)
      actuation.setActionError(null)
    },
    [queries.remediationJobsQuery.data?.jobs, remediationJob, remediationJobId, actuation],
  )

  const handleOpenRemediationSession = useCallback(
    (jobId: string) => {
      const job =
        queries.remediationJobsQuery.data?.jobs?.find(j => j.id === jobId) ??
        (remediationJobId === jobId ? remediationJob : null)
      if (onOpenAgentDesk != null) {
        onOpenAgentDesk(jobId)
        actuation.setActionError(null)
        return
      }
      if (job != null) setRemediationJob(job)
      setRemediationJobId(jobId)
      setRemediationPanelOpen(true)
      actuation.setActionError(null)
    },
    [
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
    playbookFixMutation,
    remediationStartMutation,
    remediationCancelMutation,
    handleAutoRemediate,
    followAmbientRemediationJob,
    handleOpenRemediationSession,
    handleRemediationComplete,
  }
}

export type ClusterRemediationMutations = ReturnType<typeof useClusterRemediationMutations>
