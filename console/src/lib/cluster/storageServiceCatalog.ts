import type { ClusterWorkload } from '@/api/types'
import {
  computeNamespaceReadyStats,
  computeReadyTones,
  type NamespaceReadyStats,
  type ReadyTone,
} from '@/lib/cluster/workloadReadyStats'

export type StorageServiceId = 'cnpg' | 'redis' | 'minio'

const CNPG_OPERATOR_DEPLOYMENT = 'cnpg-controller-manager'
const CNPG_CLUSTER_POD_PREFIX = 'bifrost-postgres-'
const REDIS_DEPLOYMENT_PREFIX = 'redis-'
const MINIO_DEPLOYMENT = 'minio'

export type StorageServiceDef = {
  id: StorageServiceId
  label: string
  role: string
  description: string
  sourceNamespaces: string[]
  matchWorkload: (workload: ClusterWorkload) => boolean
}

export const STORAGE_SERVICES: StorageServiceDef[] = [
  {
    id: 'cnpg',
    label: 'CNPG (SQL)',
    role: 'OLTP · system of record',
    description: 'CloudNativePG operator and bifrost-postgres cluster instances',
    sourceNamespaces: ['cnpg-system', 'data'],
    matchWorkload: workload => {
      if (
        workload.kind === 'Deployment' &&
        workload.namespace === 'cnpg-system' &&
        workload.name === CNPG_OPERATOR_DEPLOYMENT
      ) {
        return true
      }
      if (
        workload.kind === 'Pod' &&
        workload.namespace === 'cnpg-system' &&
        workload.name.startsWith(`${CNPG_OPERATOR_DEPLOYMENT}-`)
      ) {
        return true
      }
      if (
        workload.kind === 'Pod' &&
        workload.namespace === 'data' &&
        workload.name.startsWith(CNPG_CLUSTER_POD_PREFIX)
      ) {
        return true
      }
      return false
    },
  },
  {
    id: 'redis',
    label: 'Redis (Cache & Queue)',
    role: 'In-memory state · streams · Celery broker',
    description: 'Per-environment Redis live and queue instances in the data namespace',
    sourceNamespaces: ['data'],
    matchWorkload: workload =>
      workload.kind === 'Deployment' &&
      workload.namespace === 'data' &&
      workload.name.startsWith(REDIS_DEPLOYMENT_PREFIX),
  },
  {
    id: 'minio',
    label: 'MinIO (Object Store)',
    role: 'Backup & archive · WAL · artifacts',
    description: 'S3-compatible object storage for CNPG WAL, Redis RDB, and Tekton artifacts',
    sourceNamespaces: ['data', 'data-warehouse'],
    matchWorkload: workload =>
      workload.kind === 'Deployment' &&
      (workload.namespace === 'data' || workload.namespace === 'data-warehouse') &&
      workload.name === MINIO_DEPLOYMENT,
  },
]

export const DEFAULT_STORAGE_SERVICE: StorageServiceId = 'cnpg'

export function getStorageService(id: StorageServiceId): StorageServiceDef {
  const service = STORAGE_SERVICES.find(entry => entry.id === id)
  if (service == null) throw new Error(`Unknown storage service: ${id}`)
  return service
}

export function collectWorkloadsForStorageService(
  workloadsByNamespace: Map<string, ClusterWorkload[]>,
  serviceId: StorageServiceId,
): ClusterWorkload[] {
  const service = getStorageService(serviceId)
  const result: ClusterWorkload[] = []

  for (const namespace of service.sourceNamespaces) {
    const workloads = workloadsByNamespace.get(namespace) ?? []
    for (const workload of workloads) {
      if (service.matchWorkload(workload)) result.push(workload)
    }
  }

  return result
}

export type StorageServiceChipStats = {
  podCount: number
  failingPods: number
  readyStats: NamespaceReadyStats
  readyTones: { deployment: ReadyTone; standalone: ReadyTone }
}

export function computeStorageServiceChipStats(
  workloads: ClusterWorkload[],
): StorageServiceChipStats {
  const pods = workloads.filter(workload => workload.kind === 'Pod')
  const readyStats = computeNamespaceReadyStats(workloads)
  return {
    podCount: pods.length,
    failingPods: pods.filter(pod => pod.reachability === 'fail' || pod.status === 'Failed').length,
    readyStats,
    readyTones: computeReadyTones(workloads, readyStats),
  }
}

export type CnpgServiceView = {
  operator: { deployment: ClusterWorkload; pods: ClusterWorkload[] } | null
  instances: ClusterWorkload[]
}

export function buildCnpgServiceView(workloads: ClusterWorkload[]): CnpgServiceView {
  const deployments = workloads.filter(workload => workload.kind === 'Deployment')
  const pods = workloads.filter(workload => workload.kind === 'Pod')
  const operatorDeployment = deployments.find(deployment => deployment.name === CNPG_OPERATOR_DEPLOYMENT)

  const operatorPods = operatorDeployment
    ? pods.filter(pod => pod.name.startsWith(`${CNPG_OPERATOR_DEPLOYMENT}-`))
    : []

  const instances = pods
    .filter(pod => pod.name.startsWith(CNPG_CLUSTER_POD_PREFIX))
    .sort((a, b) => a.name.localeCompare(b.name))

  return {
    operator: operatorDeployment
      ? { deployment: operatorDeployment, pods: operatorPods }
      : null,
    instances,
  }
}

export function storageServiceChipTitle(service: StorageServiceDef, stats: StorageServiceChipStats): string {
  const parts = [service.label, service.role, `${stats.podCount} pods`]
  if (stats.failingPods > 0) parts.push(`${stats.failingPods} failing`)
  parts.push(`K8s: ${service.sourceNamespaces.join(', ')}`)
  return parts.join(' · ')
}

/** Aggregate deployment readiness across all storage services (for Storage tab segment badge). */
export function aggregateStorageCategoryDeployStats(
  workloadsByNamespace: Map<string, ClusterWorkload[]>,
  loadingNamespaces: Set<string>,
): { ok: number; failed: number; loading: boolean } {
  let ok = 0
  let failed = 0
  let loading = false

  for (const service of STORAGE_SERVICES) {
    if (service.sourceNamespaces.some(namespace => loadingNamespaces.has(namespace))) {
      loading = true
    }
    const workloads = collectWorkloadsForStorageService(workloadsByNamespace, service.id)
    const stats = computeNamespaceReadyStats(workloads)
    ok += stats.deploymentOk
    failed += stats.deploymentFailed
  }

  return { ok, failed, loading }
}
