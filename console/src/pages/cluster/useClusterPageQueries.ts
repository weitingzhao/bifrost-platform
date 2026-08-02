import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useMemo, useState } from 'react'
import {
  fetchCluster,
  fetchClusterEvents,
  fetchClusterGovernance,
  fetchClusterMetrics,
  fetchClusterServiceReadiness,
  fetchClusterPostgresStatus,
  fetchClusterRedisStatus,
  fetchClusterNamespaces,
  fetchClusterNodes,
  fetchClusterPlacement,
  fetchClusterObservability,
  fetchClusterWorkloads,
  fetchJoinProfiles,
  fetchNodePower,
} from '@/api/cluster'
import { fetchPodLogs } from '@/api/clusterActuation'
import { fetchContext } from '@/api/core'
import { fetchRemediationJobs } from '@/api/remediation'
import type { ClusterNode, ClusterWorkload } from '@/api/clusterTypes'
import { DEPRECATED_NAMESPACES } from '@/components/cluster/ClusterWorkloadsExplorer'
import { bifrostNamespacesReady, clusterBootstrapNeedsActions } from '@/lib/cluster/clusterBootstrap'
import type { ClusterCategory } from '@/lib/cluster/clusterCategories'
import {
  INFRASTRUCTURE_CATEGORY_LABELS,
  isInfrastructureCategory,
} from '@/lib/cluster/clusterCategories'
import { buildClusterCategoryLlmContext } from '@/lib/cluster/buildClusterCategoryLlmContext'
import { buildClusterLlmContext } from '@/lib/cluster/buildClusterLlmContext'
import type { NodeWizardFlow } from '@/lib/cluster/nodeWizard'
import { findActiveRemediationJob } from '@/lib/remediation/remediationJobDisplay'

export type CopyState = 'idle' | 'copied' | 'error'

export interface ClusterPageQueriesInput {
  selectedNs: string | null
  selectedPod: string | null
  drawerOpen: boolean
  selectedNode: ClusterNode | null
  nodeDrawerOpen: boolean
  wizardFlow: NodeWizardFlow
  pinnedWorkload: ClusterWorkload | null
  selectedCategory: ClusterCategory | null
}

function formatUpdatedAt(ms: number): string {
  if (!ms) return '—'
  return new Date(ms).toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

export function useClusterPageQueries(input: ClusterPageQueriesInput) {
  const {
    selectedNs,
    selectedPod,
    drawerOpen,
    selectedNode,
    nodeDrawerOpen,
    wizardFlow,
    pinnedWorkload,
    selectedCategory,
  } = input

  const qc = useQueryClient()
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const [categoryCopyId, setCategoryCopyId] = useState<ClusterCategory | null>(null)
  const [categoryCopyState, setCategoryCopyState] = useState<CopyState>('idle')

  const summaryQuery = useQuery({
    queryKey: ['cluster', 'summary'],
    queryFn: fetchCluster,
    refetchInterval: 30_000,
  })

  const nodesQuery = useQuery({
    queryKey: ['cluster', 'nodes'],
    queryFn: fetchClusterNodes,
    refetchInterval: 30_000,
  })

  const clusterNodes = nodesQuery.data?.nodes ?? []

  /** Merge latest probe fields (unschedulable, status) — selectedNode state alone goes stale after actuation. */
  const selectedNodeLive = useMemo(() => {
    if (selectedNode?.name == null) return null
    return clusterNodes.find(n => n.name === selectedNode.name) ?? selectedNode
  }, [clusterNodes, selectedNode])

  const metricsQuery = useQuery({
    queryKey: ['cluster', 'metrics'],
    queryFn: () => fetchClusterMetrics(8),
    refetchInterval: 30_000,
  })

  const observabilityQuery = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 30_000,
    retry: false,
  })

  const governanceQuery = useQuery({
    queryKey: ['cluster', 'governance'],
    queryFn: fetchClusterGovernance,
    refetchInterval: 30_000,
    retry: false,
  })

  const serviceReadinessQuery = useQuery({
    queryKey: ['cluster', 'service-readiness'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: 30_000,
    retry: false,
  })

  const postgresStatusQuery = useQuery({
    queryKey: ['cluster', 'postgres'],
    queryFn: fetchClusterPostgresStatus,
    refetchInterval: 30_000,
    retry: false,
  })

  const redisStatusQuery = useQuery({
    queryKey: ['cluster', 'redis'],
    queryFn: fetchClusterRedisStatus,
    refetchInterval: 30_000,
    retry: false,
  })

  const remediationJobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 15_000,
  })

  const activeRemediationJob = useMemo(
    () => findActiveRemediationJob(remediationJobsQuery.data?.jobs ?? []),
    [remediationJobsQuery.data?.jobs],
  )

  const clusterFetching =
    summaryQuery.isFetching ||
    nodesQuery.isFetching ||
    metricsQuery.isFetching ||
    observabilityQuery.isFetching

  const namespacesQuery = useQuery({
    queryKey: ['cluster', 'namespaces'],
    queryFn: () => fetchClusterNamespaces(''),
    refetchInterval: 30_000,
  })

  const placementQuery = useQuery({
    queryKey: ['cluster', 'placement'],
    queryFn: fetchClusterPlacement,
    refetchInterval: 30_000,
  })

  const workloadsQuery = useQuery({
    queryKey: ['cluster', 'workloads', selectedNs],
    queryFn: () => fetchClusterWorkloads(selectedNs ?? 'default'),
    enabled: selectedNs != null,
    refetchInterval: 30_000,
  })

  const eventsQuery = useQuery({
    queryKey: ['cluster', 'events', selectedNs],
    queryFn: () => fetchClusterEvents(selectedNs ?? undefined, 50),
    enabled: selectedNs != null && drawerOpen,
    refetchInterval: 30_000,
  })

  const logsQuery = useQuery({
    queryKey: ['cluster', 'pod-logs', selectedNs, selectedPod],
    queryFn: () => fetchPodLogs(selectedNs ?? '', selectedPod ?? '', 200),
    enabled: selectedNs != null && selectedPod != null && drawerOpen,
    refetchInterval: 30_000,
  })

  const joinProfilesQuery = useQuery({
    queryKey: ['cluster', 'join-profiles'],
    queryFn: fetchJoinProfiles,
    refetchInterval: 60_000,
  })

  const nodePowerQuery = useQuery({
    queryKey: ['cluster', 'node-power', selectedNodeLive?.name],
    queryFn: () => fetchNodePower(selectedNodeLive?.name ?? ''),
    enabled:
      selectedNodeLive?.compute_managed === true &&
      selectedNodeLive.name != null &&
      (nodeDrawerOpen || wizardFlow !== 'join'),
    refetchInterval: 15_000,
  })

  const selectedWorkload = useMemo(() => {
    if (selectedPod == null || selectedNs == null) return undefined
    if (
      pinnedWorkload != null &&
      pinnedWorkload.name === selectedPod &&
      pinnedWorkload.namespace === selectedNs
    ) {
      return pinnedWorkload
    }
    return workloadsQuery.data?.workloads.find(
      workload => workload.name === selectedPod && workload.namespace === selectedNs,
    )
  }, [workloadsQuery.data, selectedNs, selectedPod, pinnedWorkload])

  const podEvents = useMemo(() => {
    if (selectedPod == null) return []
    return (eventsQuery.data?.events ?? []).filter(e => e.object?.includes(selectedPod) ?? false)
  }, [eventsQuery.data, selectedPod])

  const unreachable =
    summaryQuery.data?.reachability === 'fail' &&
    (summaryQuery.data?.detail?.includes('kubeconfig') ?? false)

  const metricsOk = metricsQuery.data?.metrics_server_available === true
  const clusterSummary = summaryQuery.data
  const selectedCategoryTitle = useMemo(() => {
    if (selectedCategory == null) return undefined
    if (isInfrastructureCategory(selectedCategory)) {
      return INFRASTRUCTURE_CATEGORY_LABELS[selectedCategory]
    }
    return serviceReadinessQuery.data?.domains.find(d => d.id === selectedCategory)?.label
  }, [selectedCategory, serviceReadinessQuery.data?.domains])

  // bifrost* filter is for UI lists only — bootstrap completion must use the full inventory
  // (CORE includes cicd + monitoring, which do not start with "bifrost").
  const allNamespaces = namespacesQuery.data?.namespaces
  const bifrostNamespaces = allNamespaces?.filter(ns => ns.name.startsWith('bifrost')) ?? []
  const visibleNamespaces = useMemo(
    () => (namespacesQuery.data?.namespaces ?? []).filter(ns => !DEPRECATED_NAMESPACES.has(ns.name)),
    [namespacesQuery.data?.namespaces],
  )

  const showBootstrapActions = clusterBootstrapNeedsActions(metricsOk, allNamespaces)
  const bifrostNsReady = bifrostNamespacesReady(allNamespaces)
  const clusterSummaryError =
    summaryQuery.isError && summaryQuery.error instanceof Error
      ? summaryQuery.error.message
      : summaryQuery.isError
        ? 'Cluster summary request failed'
        : null
  const clusterStatusLabel =
    clusterSummary?.label ??
    (summaryQuery.isError
      ? 'Cluster unreachable'
      : summaryQuery.isPending
        ? 'Loading…'
        : 'No cluster summary')
  const clusterUpdatedAt =
    nodesQuery.dataUpdatedAt > 0 ? formatUpdatedAt(nodesQuery.dataUpdatedAt) : null

  const clusterLlmInput = useMemo(
    () => ({
      summary: summaryQuery.data,
      nodes: nodesQuery.data?.nodes,
      governance: governanceQuery.data,
      serviceReadiness: serviceReadinessQuery.data,
      metrics: metricsQuery.data,
      namespaces: namespacesQuery.data?.namespaces,
      placement: placementQuery.data,
      observability: observabilityQuery.data,
      selectedNamespace: selectedNs,
      workloads: workloadsQuery.data?.workloads,
    }),
    [
      governanceQuery.data,
      serviceReadinessQuery.data,
      metricsQuery.data,
      namespacesQuery.data?.namespaces,
      nodesQuery.data?.nodes,
      observabilityQuery.data,
      placementQuery.data,
      selectedNs,
      summaryQuery.data,
      workloadsQuery.data?.workloads,
    ],
  )

  function refreshCluster() {
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
  }

  const handleCopyForLlm = useCallback(async () => {
    const namespaces = clusterLlmInput.namespaces
    const text = buildClusterLlmContext({
      ...clusterLlmInput,
      namespaces,
    })

    try {
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [clusterLlmInput])

  const handleCopyCategoryForLlm = useCallback(
    async (category: ClusterCategory, categoryTitle: string) => {
      const namespaces = clusterLlmInput.namespaces

      let opsContext
      let postgresStatus
      let redisStatus
      if (category === 'database' || category === 'redis') {
        try {
          opsContext = await fetchContext()
        } catch {
          /* live probes only */
        }
      }
      if (category === 'database') {
        try {
          postgresStatus = await fetchClusterPostgresStatus()
        } catch {
          /* live probes only */
        }
      }
      if (category === 'redis') {
        try {
          redisStatus = await fetchClusterRedisStatus()
        } catch {
          /* live probes only */
        }
      }

      const text = buildClusterCategoryLlmContext({
        ...clusterLlmInput,
        namespaces,
        category,
        categoryTitle,
        opsContext,
        postgresStatus,
        redisStatus,
      })

      try {
        await navigator.clipboard.writeText(text)
        setCategoryCopyId(category)
        setCategoryCopyState('copied')
        window.setTimeout(() => {
          setCategoryCopyState('idle')
          setCategoryCopyId(null)
        }, 2000)
      } catch {
        setCategoryCopyId(category)
        setCategoryCopyState('error')
        window.setTimeout(() => {
          setCategoryCopyState('idle')
          setCategoryCopyId(null)
        }, 3000)
      }
    },
    [clusterLlmInput],
  )

  return {
    qc,
    summaryQuery,
    nodesQuery,
    metricsQuery,
    observabilityQuery,
    governanceQuery,
    serviceReadinessQuery,
    postgresStatusQuery,
    redisStatusQuery,
    remediationJobsQuery,
    namespacesQuery,
    placementQuery,
    workloadsQuery,
    eventsQuery,
    logsQuery,
    joinProfilesQuery,
    nodePowerQuery,
    clusterNodes,
    selectedNodeLive,
    selectedWorkload,
    podEvents,
    activeRemediationJob,
    clusterFetching,
    unreachable,
    metricsOk,
    clusterSummary,
    selectedCategoryTitle,
    bifrostNamespaces,
    visibleNamespaces,
    showBootstrapActions,
    bifrostNsReady,
    clusterStatusLabel,
    clusterSummaryError,
    clusterUpdatedAt,
    clusterLlmInput,
    copyState,
    categoryCopyId,
    categoryCopyState,
    refreshCluster,
    handleCopyForLlm,
    handleCopyCategoryForLlm,
  }
}

export type ClusterPageQueries = ReturnType<typeof useClusterPageQueries>
