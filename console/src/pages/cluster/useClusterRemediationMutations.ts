import { useMutation } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { fetchAgentBridge } from '@/api/agentOps'
import { fetchMatrix, fetchSelfHealth, isAllMatrices } from '@/api/core'
import { fetchSupplyChain } from '@/api/delivery'
import { fetchStgSmoke } from '@/api/promote'
import { startRemediation, cancelRemediationJob, fetchRemediationHealth } from '@/api/remediation'
import type { RemediationJob } from '@/api/remediationTypes'
import { CLUSTER_ISSUES_FULL_AUTO_SCOPE } from '@/lib/agent/agentScopes'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { buildClusterAutoCheckBundle } from '@/lib/cluster/buildClusterAutoCheckPrompt'
import { buildClusterLlmContext } from '@/lib/cluster/buildClusterLlmContext'
import type { ClusterMutationActuation, ClusterPageMutationsInput } from './clusterMutationTypes'

function settledValue<T>(p: PromiseSettledResult<T>): T | undefined {
  return p.status === 'fulfilled' ? p.value : undefined
}

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
    | 'onExpandAgentDock'
    | 'onSelectAgentJob'
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
    onExpandAgentDock,
    onSelectAgentJob,
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
        setRemediationPanelOpen(false)
        setRemediationJobId(job.id)
        setRemediationJob(job)
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
      if (onStartAgentJob != null) {
        const scope = job.scope ?? CLUSTER_ISSUES_FULL_AUTO_SCOPE
        setRemediationPanelOpen(false)
        setRemediationJobId(job.id)
        setRemediationJob(job)
        onStartAgentJob({ id: job.id, scope, label: scopeToLabel(scope) })
        actuation.setActionError(null)
        return
      }
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

    void (async () => {
      const [supplyR, smokeR, selfR, runnerR, bridgeR, matrixR] = await Promise.allSettled([
        fetchSupplyChain(),
        fetchStgSmoke(),
        fetchSelfHealth(),
        fetchRemediationHealth(),
        fetchAgentBridge(),
        fetchMatrix(),
      ])

      const matrixData = settledValue(matrixR)
      const matrices =
        matrixData == null ? [] : isAllMatrices(matrixData) ? matrixData.matrices : [matrixData]

      const bundle = buildClusterAutoCheckBundle({
        summary: clusterSummary,
        serviceReadiness,
        postgresStatus,
        governance,
        supplyChain: settledValue(supplyR),
        stgSmoke: settledValue(smokeR),
        selfHealth: settledValue(selfR),
        runnerHealth: settledValue(runnerR),
        agentBridge: settledValue(bridgeR),
        matrices,
        clusterLlmContext: buildClusterLlmContext({
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

      remediationStartMutation.mutate({
        scope: CLUSTER_ISSUES_FULL_AUTO_SCOPE,
        cluster_summary: clusterSummary,
        service_readiness: serviceReadiness,
        governance,
        issues: bundle.fleetIssues,
        prompt: bundle.prompt,
      })
    })()
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
