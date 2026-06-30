/**
 * Data Layer catalog — Redis & PostgreSQL architecture principles for K3s.
 *
 * Authoritative source for Ops Console → Architecture → K3s → Data Layer.
 * Complements K3s Architecture (topology/CI/CD) with stateful-service design.
 * Aligned with Vision § Redis Ideal Topology.
 */

import type { OpsContextResponse } from '@/api/types'
import { projectWaveStatus } from '@/lib/briefing/waveProjection'

export const DATA_LAYER_VERSION = '2026-06-20'
export const DATA_LAYER_SOURCE = 'console/src/lib/architecture/dataLayerCatalog.ts'

export const DATA_LAYER_MIGRATE_STREAM_ID = 'data-layer-k3s'

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
  { dimension: 'Storage', principle: 'local-path PVC on NVMe', note: 'ubt-k3s-02 prod-pool local disk for max IO — not nfs-hot for PGDATA' },
  { dimension: 'Scheduling', principle: 'nodeAffinity → ubt-k3s-02 (prod-pool)', note: 'Label node-role=postgres; Standby on ubt-k3s-04 or ubt-k3s-01' },
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
// K3s data layer migration (Agent Briefing + spine stream data-layer-k3s)
// ---------------------------------------------------------------------------

export type DataLayerPhaseStatus = 'pending' | 'next' | 'in_progress' | 'done'

export type DataLayerMigrationPhase = {
  id: string
  step: number
  /** D-C: position in spine done count (step - 1). */
  spineIndex: number
  /** Display prefix in queue / next_task (①..⑦). */
  displayCode: string
  label: string
  repo: string
  verify: string
  blockedBy?: string
}

/** Seven phases — keep in sync with ops-context.yaml tracks.migrate.streams data-layer-k3s total: 7 */
export const DATA_LAYER_MIGRATION_PHASES: DataLayerMigrationPhase[] = [
  {
    id: 'data-0-cnpg-operator',
    step: 1,
    spineIndex: 0,
    displayCode: '①',
    label: 'Label ubt-k3s-02 postgres-role + deploy CloudNativePG operator + bifrost-postgres cluster (data NS)',
    repo: 'bifrost-trade-infra/k8s/data/ + scripts/k3s/install-data-layer-phase0.sh',
    verify: 'kubectl get cluster -n data; postgres-role capability ready on ubt-k3s-02',
  },
  {
    id: 'data-1-minio-backup',
    step: 2,
    spineIndex: 1,
    displayCode: '②',
    label: 'MinIO backup target (nfs-hot) + CNPG barmanObjectStore WAL archive',
    repo: 'bifrost-trade-infra/k8s/data/minio/ · nfs-hot StorageClass',
    verify: 'CNPG backup status OK; test WAL archive to nfs-hot bucket',
    blockedBy: 'data-0-cnpg-operator',
  },
  {
    id: 'data-2-stg-cutover',
    step: 3,
    spineIndex: 2,
    displayCode: '③',
    label: 'STG cutover — apps connect bifrost-postgres-rw.data.svc + redis-live/queue-stg; remove bifrost-stg in-ns postgres/redis',
    repo: 'bifrost-trade-infra/k8s/overlays/stg/',
    verify: 'bifrost-stg daemon_control + IB ingestor Stream + deliver-stg smoke pass',
    blockedBy: 'data-1-minio-backup',
  },
  {
    id: 'data-3-dev-cutover',
    step: 4,
    spineIndex: 3,
    displayCode: '④',
    label: 'DEV cutover — bifrost-dev config → data NS endpoints; remove bifrost-dev in-ns postgres/redis',
    repo: 'bifrost-trade-infra/k8s/overlays/dev/',
    verify: 'Vision V1 gate :30882 + bifrost_dev schema via CNPG',
    blockedBy: 'data-2-stg-cutover',
  },
  {
    id: 'data-4-prod-pg',
    step: 5,
    spineIndex: 4,
    displayCode: '⑤',
    label: 'PROD PG migrate — pg_dump legacy .80 → CNPG bifrost_prod; maintenance window + rollback plan',
    repo: 'bifrost-trade-infra/k8s/overlays/prod/config/',
    verify: 'make prod-health; monitor daemon_control; D2-prime cutover sign-off',
    blockedBy: 'data-3-dev-cutover',
  },
  {
    id: 'data-5-redis-split',
    step: 6,
    spineIndex: 5,
    displayCode: '⑥',
    label: 'PROD/STG redis-live + redis-queue split (Bitnami HA); Celery → redis-queue only',
    repo: 'bifrost-trade-infra/k8s/data/redis/',
    verify: 'noeviction on live; Celery bars queue isolated; NetworkPolicy per env',
    blockedBy: 'data-4-prod-pg',
  },
  {
    id: 'data-6-retire-embedded',
    step: 7,
    spineIndex: 6,
    displayCode: '⑦',
    label: 'Retire embedded stateful — remove postgres/redis from bifrost-* base; bare .80 PG standby or offline',
    repo: 'bifrost-trade-infra/k8s/base/ · bifrost-platform/config/environments.yaml',
    verify: 'data NS only; matrix probes point at cluster endpoints; legacy .80 read-only or decommissioned',
    blockedBy: 'data-5-redis-split',
  },
]

export const DATA_LAYER_SESSION_CONSTRAINTS: string[] = [
  'PG hot storage: local-path on postgres node (ubt-k3s-02) — NOT nfs-hot for PGDATA',
  'NAS nfs-hot / nfs-cold: WAL/RDB backups and cold archive only (Retain reclaim)',
  'R-DV1: bifrost_dev / bifrost_stg / bifrost_prod (or options_db alias) — separate Redis instances per env',
  'Single-variable: complete stg cutover before prod PG migration',
  'Prod PG cutover requires Owner maintenance window — no parallel compose→k3s changes',
  'Remove per-namespace postgres/redis Deployments from bifrost-{dev,stg,prod} after each env cutover',
]

/** Index of the active (recommended) phase from spine stream progress (done = completed count). */
export function activeDataLayerPhaseIndex(ctx?: OpsContextResponse): number {
  const stream = ctx?.tracks?.migrate?.streams.find(s => s.id === DATA_LAYER_MIGRATE_STREAM_ID)
  if (stream == null) return 0
  if (stream.status === 'closed' || stream.status === 'signed') return DATA_LAYER_MIGRATION_PHASES.length
  return Math.min(Math.max(stream.done, 0), DATA_LAYER_MIGRATION_PHASES.length - 1)
}

export function activeDataLayerPhase(ctx?: OpsContextResponse): DataLayerMigrationPhase | undefined {
  const idx = activeDataLayerPhaseIndex(ctx)
  if (idx >= DATA_LAYER_MIGRATION_PHASES.length) return undefined
  return DATA_LAYER_MIGRATION_PHASES[idx]
}

/** Agent Briefing appendix — phased migration queue aligned with spine stream data-layer-k3s. */
export function formatDataLayerBriefingAppendix(ctx?: OpsContextResponse): string {
  const stream = ctx?.tracks?.migrate?.streams.find(s => s.id === DATA_LAYER_MIGRATE_STREAM_ID)
  const lines = [
    '## Data layer migration phases (K3s)',
    '',
    `Source: ${DATA_LAYER_SOURCE} · spine stream \`${DATA_LAYER_MIGRATE_STREAM_ID}\``,
    stream != null
      ? `Spine progress: ${stream.done}/${stream.total} · status=${stream.status}${stream.next_task != null ? ` · next: ${stream.next_task}` : ''}`
      : 'Spine stream: (not loaded — use phases below)',
    '',
    'Authority: decision **D2-prime** supersedes D2 (.80 bare-metal interim).',
    '',
    '### Phases (①–⑦)',
  ]

  for (const p of DATA_LAYER_MIGRATION_PHASES) {
    const projected =
      stream != null
        ? projectWaveStatus(p.spineIndex, {
            done: stream.done,
            readyForSignoff: stream.ready_for_signoff ?? 0,
            streamStatus: stream.status,
          })
        : 'pending'
    const marker =
      projected === 'next'
        ? ' *(spine next)*'
        : projected === 'ready_for_signoff'
          ? ' — ✅ DELIVERED, awaiting Owner sign-off'
          : projected === 'done'
            ? ' — ✔ signed'
            : ''
    lines.push(`${p.displayCode}. **${p.label}**${marker}`)
    lines.push(`   - id: ${p.id} · repo: ${p.repo}`)
    lines.push(`   - verify: ${p.verify}`)
    if (p.blockedBy) lines.push(`   - blocked_by: ${p.blockedBy}`)
    lines.push('')
  }

  lines.push('### Session constraints')
  for (const c of DATA_LAYER_SESSION_CONSTRAINTS) lines.push(`- ${c}`)
  return lines.join('\n')
}

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
    '',
    '## Migration phases (data-layer-k3s stream)',
    ...DATA_LAYER_MIGRATION_PHASES.map(
      p => `${p.step}. **${p.id}**: ${p.label} · verify: ${p.verify}`,
    ),
    '',
    '## Session constraints',
    ...DATA_LAYER_SESSION_CONSTRAINTS.map(c => `- ${c}`),
  ]
  return lines.join('\n')
}
