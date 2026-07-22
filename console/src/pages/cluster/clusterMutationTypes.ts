import type {
  ClusterGovernanceResponse,
  ClusterNode,
  ClusterObservabilityResponse,
  ClusterPostgresStatusResponse,
  ClusterServiceReadinessResponse,
  ClusterSummary,
  ClusterWorkload,
  JoinProfilesResponse,
} from '@/api/clusterTypes'
import type { QueryClient } from '@tanstack/react-query'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import type { ClusterPageQueries } from './useClusterPageQueries'

export interface ConfirmState {
  open: boolean
  title: string
  message: string
  confirmLabel: string
  action: () => void
}

export interface ScaleState {
  workload: ClusterWorkload
  replicas: number
}

export interface ClusterPageMutationsInput {
  selectedNode: ClusterNode | null
  wizardJoinProfileId: string | null
  joinProfiles: JoinProfilesResponse | undefined
  canAdmin: boolean
  observability: ClusterObservabilityResponse | undefined
  clusterSummary: ClusterSummary | undefined
  serviceReadiness: ClusterServiceReadinessResponse | undefined
  governance: ClusterGovernanceResponse | undefined
  postgresStatus: ClusterPostgresStatusResponse | undefined
  queries: Pick<
    ClusterPageQueries,
    | 'nodesQuery'
    | 'metricsQuery'
    | 'namespacesQuery'
    | 'placementQuery'
    | 'observabilityQuery'
    | 'workloadsQuery'
    | 'remediationJobsQuery'
  >
  selectedNs: string | null
  onOpenAgentDesk?: (arg?: string | { prefill: string }) => void
  onStartAgentJob?: (job: AmbientAgentJob) => void
  setDrawerOpen: (open: boolean) => void
  setSelectedPod: (name: string | null) => void
}

export interface ClusterMutationActuation {
  handleActuationSuccess: (message: string) => void
  handleActuationError: (err: Error) => void
  requireConfirm: (next: Omit<ConfirmState, 'open'>) => void
  setActionError: (message: string | null) => void
  setScaleState: (state: ScaleState | null) => void
  qc: QueryClient
}
