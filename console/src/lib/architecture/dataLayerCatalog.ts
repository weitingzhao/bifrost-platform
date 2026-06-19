/**
 * Data Layer catalog — Redis & PostgreSQL architecture principles for K3s.
 *
 * Authoritative source for Ops Console → Architecture → K3s → Data Layer.
 * Complements K3s Architecture (topology/CI/CD) with stateful-service design.
 * Aligned with Vision § Redis Ideal Topology.
 */

export const DATA_LAYER_VERSION = '2026-06-19'
export const DATA_LAYER_SOURCE = 'console/src/lib/architecture/dataLayerCatalog.ts'

// ---------------------------------------------------------------------------
// Redis architecture
// ---------------------------------------------------------------------------

export type RedisInstanceDef = {
  name: string
  roles: string
  maxmemoryPolicy: string
  persistence: string
  ha: string
}

export const REDIS_INSTANCES: RedisInstanceDef[] = [
  {
    name: 'redis-live',
    roles: 'R1 Realtime quotes · R2 Operator RPC · R3 Account stream · R4 Health · R5 Console logs',
    maxmemoryPolicy: 'noeviction',
    persistence: 'AOF appendfsync everysec',
    ha: 'Primary + Replica + 3 Sentinel (quorum across mini-pc-a/b)',
  },
  {
    name: 'redis-queue',
    roles: 'R6 Celery broker + result backend',
    maxmemoryPolicy: 'allkeys-lru or volatile-lru',
    persistence: 'AOF or RDB+AOF (task results can expire)',
    ha: 'Primary + Replica + Sentinel (same quorum)',
  },
]

export type RedisEnvIsolationRow = {
  environment: string
  liveInstance: string
  queueInstance: string
  networkPolicy: string
}

export const REDIS_ENV_ISOLATION: RedisEnvIsolationRow[] = [
  {
    environment: 'prod',
    liveInstance: 'redis-live-prod.data.svc',
    queueInstance: 'redis-queue-prod.data.svc',
    networkPolicy: 'Only bifrost-prod Pods egress allowed',
  },
  {
    environment: 'stg',
    liveInstance: 'redis-live-stg.data.svc',
    queueInstance: 'redis-queue-stg.data.svc',
    networkPolicy: 'Only bifrost-stg Pods egress allowed',
  },
  {
    environment: 'dev',
    liveInstance: 'Local (Mac docker / brew) or redis-dev.data.svc',
    queueInstance: 'Same instance db=1 (dev simplicity)',
    networkPolicy: 'Never writes Prod/STG Redis',
  },
]

export type RedisDeployPrinciple = {
  dimension: string
  principle: string
  note: string
}

export const REDIS_DEPLOY_PRINCIPLES: RedisDeployPrinciple[] = [
  { dimension: 'Helm chart', principle: 'Bitnami Redis (replication + sentinel mode)', note: 'Not Redis Cluster — simpler for Stream + Pub/Sub' },
  { dimension: 'Namespace', principle: 'data (shared stateful namespace)', note: 'Colocated with PG; dedicated from compute workloads' },
  { dimension: 'Node binding', principle: 'Primary on mini-pc-b; Replica on mini-pc-a', note: 'Matches PG placement for data-node affinity' },
  { dimension: 'Storage', principle: 'local-path PVC + AOF', note: 'NVMe IO; PVC snapshot for backup' },
  { dimension: 'Auth', principle: 'requirepass via K8s Secret', note: 'Apps inject REDIS_LIVE_URL / REDIS_QUEUE_URL' },
  { dimension: 'Monitoring', principle: 'redis_memory, connected_clients, stream_groups lag, replication_offset', note: 'Prometheus redis-exporter sidecar' },
  { dimension: 'Backup', principle: 'Periodic redis-cli --rdb → MinIO (live priority)', note: 'Supplement AOF with point-in-time snapshots' },
]

// ---------------------------------------------------------------------------
// PostgreSQL architecture (extracted & extended from K3s Architecture)
// ---------------------------------------------------------------------------

export type PgPrinciple = {
  dimension: string
  principle: string
  note: string
}

export const PG_DEPLOY_PRINCIPLES: PgPrinciple[] = [
  { dimension: 'Operator', principle: 'CloudNativePG', note: 'Declarative YAML; operator manages lifecycle + failover' },
  { dimension: 'Storage', principle: 'local-path PVC on NVMe', note: 'mini-pc-b dedicated DB node for max IO' },
  { dimension: 'Scheduling', principle: 'nodeAffinity → mini-pc-b', note: 'Primary always on DB node; Standby on mini-pc-a' },
  { dimension: 'Instances', principle: '2 (Primary + Standby)', note: 'Streaming replication; auto failover' },
  { dimension: 'Backup', principle: 'WAL archive → MinIO (barmanObjectStore)', note: 'PITR capable; daily base backup' },
  { dimension: 'Databases', principle: 'bifrost_dev / bifrost_stg / bifrost_prod (R-DV1)', note: 'Same cluster, logical isolation; apps connect via db name' },
  { dimension: 'Connection', principle: 'bifrost-postgres-rw.data.svc.cluster.local:5432', note: 'Apps use RW service; Standby via -ro service' },
  { dimension: 'Parameters', principle: 'shared_buffers=8GB, max_connections=200', note: 'Tuned for 32GB RAM mini-pc-b' },
]

// ---------------------------------------------------------------------------
// MinIO / Object Storage (future)
// ---------------------------------------------------------------------------

export type MinioRole = {
  purpose: string
  bucket: string
  consumers: string
}

export const MINIO_ROLES: MinioRole[] = [
  { purpose: 'PG WAL archive', bucket: 's3://postgres-backup', consumers: 'CloudNativePG barman' },
  { purpose: 'Redis RDB snapshots', bucket: 's3://redis-backup', consumers: 'CronJob redis-cli --rdb' },
  { purpose: 'Tekton artifacts', bucket: 's3://tekton-artifacts', consumers: 'Tekton Pipeline results' },
  { purpose: 'ML model storage (future)', bucket: 's3://models', consumers: 'Ollama / AI namespace' },
]

// ---------------------------------------------------------------------------
// Data layer responsibility split (Redis vs PG)
// ---------------------------------------------------------------------------

export type ResponsibilitySplit = {
  concern: string
  redis: string
  pg: string
}

export const DATA_RESPONSIBILITY: ResponsibilitySplit[] = [
  { concern: 'Real-time quotes', redis: 'Source of truth (tick-level, ephemeral)', pg: 'N/A' },
  { concern: 'Trade commands (RPC)', redis: 'Transport (Stream + ACK)', pg: 'Audit log (after execution)' },
  { concern: 'Account positions', redis: 'Staging bus (event stream)', pg: 'Business truth (synced by Account Sync)' },
  { concern: 'Daemon state', redis: 'Health hash (live lamp)', pg: 'Historical snapshots (status sink)' },
  { concern: 'Strategy config', redis: 'N/A', pg: 'Source of truth (gate_safety_*, strategy_*)' },
  { concern: 'Celery tasks', redis: 'Broker + result', pg: 'Job history (job_bars_backfill)' },
  { concern: 'Trade history', redis: 'N/A', pg: 'Source of truth (executions, fills)' },
]

// ---------------------------------------------------------------------------
// LLM pack
// ---------------------------------------------------------------------------

export function buildDataLayerLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — Data Layer Architecture',
    `# Source: ${DATA_LAYER_SOURCE} v${DATA_LAYER_VERSION}`,
    '',
    '## Redis instances (per environment)',
    ...REDIS_INSTANCES.map(r =>
      `- **${r.name}**: ${r.roles} | policy=${r.maxmemoryPolicy} | persist=${r.persistence} | HA=${r.ha}`),
    '',
    '## Redis environment isolation',
    ...REDIS_ENV_ISOLATION.map(r =>
      `- **${r.environment}**: live=${r.liveInstance}; queue=${r.queueInstance}; policy=${r.networkPolicy}`),
    '',
    '## Redis deployment principles',
    ...REDIS_DEPLOY_PRINCIPLES.map(r => `- **${r.dimension}**: ${r.principle} — ${r.note}`),
    '',
    '## PostgreSQL deployment principles',
    ...PG_DEPLOY_PRINCIPLES.map(p => `- **${p.dimension}**: ${p.principle} — ${p.note}`),
    '',
    '## MinIO roles',
    ...MINIO_ROLES.map(m => `- **${m.purpose}**: ${m.bucket} → ${m.consumers}`),
    '',
    '## Data responsibility split (Redis vs PG)',
    ...DATA_RESPONSIBILITY.map(d => `- **${d.concern}**: Redis=[${d.redis}] | PG=[${d.pg}]`),
  ]
  return lines.join('\n')
}
