import type {
  ClusterGovernanceResponse,
  ClusterNode,
  ClusterObservabilityResponse,
  ClusterPostgresStatusResponse,
  ClusterServiceReadinessResponse,
  ClusterSummary,
  ClusterWorkload,
} from '@/api/clusterTypes'
import type { QueryClient } from '@tanstack/react-query'

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
  canAdmin: boolean
  observability: ClusterObservabilityResponse | undefined
  clusterSummary: ClusterSummary | undefined
  serviceReadiness: ClusterServiceReadinessResponse | undefined
  governance: ClusterGovernanceResponse | undefined
  postgresStatus: ClusterPostgresStatusResponse | undefined
  selectedNs: string | null
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
