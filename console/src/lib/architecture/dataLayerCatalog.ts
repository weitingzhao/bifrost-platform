/**
 * Data Layer catalog — Redis, PostgreSQL, MinIO architecture principles for K3s (catalog-only).
 *
 * Complements k3sArchitectureCatalog.ts (topology/CI/CD) with stateful-service design.
 * Aligned with Vision § Redis Ideal Topology.
 *
 * Live state (not this catalog):
 * - PG/Redis/MinIO readiness: Rocket → Cluster (Postgres / Redis / Issues panels)
 * - Migrate progress: Engineer → Briefing · lane data-layer-k3s + spine projection
 */

import type { OpsContextResponse } from '@/api/types'
import { projectWaveStatus } from '@/lib/briefing/waveProjection'
import type { DataLayerMigrationPhase } from './dataLayerCatalogTypes'
import { GENERATED_DATA_LAYER_PHASES } from './migrateWaves.generated'

export type { DataLayerMigrationPhase } from './dataLayerCatalogTypes'
export const DATA_LAYER_MIGRATION_PHASES = GENERATED_DATA_LAYER_PHASES

export const DATA_LAYER_VERSION = '2026-06-20'
export const DATA_LAYER_SOURCE = 'config/migrate-waves/data-layer-k3s.yaml'

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
// PostgreSQL architecture (extracted & extended from k3sArchitectureCatalog.ts)
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

export const DATA_LAYER_RELATED_AUTHORITIES = [
  'Live PG/Redis/MinIO readiness: Rocket → Cluster (Postgres / Redis / Issues panels)',
  'Migrate lane + spine-projected queue: Engineer → Briefing · lane data-layer-k3s',
  'Target topology complement: k3sArchitectureCatalog.ts',
  'Spine: config/ops-context.yaml · GET /api/v1/context',
]

/** Archived migration phase definitions — live done/total/next_task uses spine + formatDataLayerBriefingAppendix(ctx). */
export function buildDataLayerHistoricalAppendix(): string {
  const lines: string[] = [
    '## Historical progress (archived phase definitions — do not treat spine counts as live here)',
    '',
    `Spine stream: \`${DATA_LAYER_MIGRATE_STREAM_ID}\` · live progress: formatDataLayerBriefingAppendix(ctx) or Agent Briefing lane.`,
    '',
    'Authority: decision **D2-prime** supersedes D2 (.80 bare-metal interim).',
    '',
    '### Migration phases (①–⑦)',
    ...DATA_LAYER_MIGRATION_PHASES.map(p => [
      `${p.displayCode}. **${p.id}**: ${p.label}`,
      `   - repo: ${p.repo}`,
      `   - verify: ${p.verify}`,
      ...(p.blockedBy != null ? [`   - blocked_by: ${p.blockedBy}`] : []),
      '',
    ]).flat(),
  ]
  return lines.join('\n')
}

export function buildDataLayerLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — Data Layer Architecture',
    `# Source: ${DATA_LAYER_SOURCE} v${DATA_LAYER_VERSION}`,
    'Live cluster + migrate progress: Rocket → Cluster / Engineer → Briefing (data-layer-k3s) — not this catalog.',
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
    '## Session constraints',
    ...DATA_LAYER_SESSION_CONSTRAINTS.map(c => `- ${c}`),
    '',
    '## Related authorities',
    ...DATA_LAYER_RELATED_AUTHORITIES.map(a => `- ${a}`),
    '',
    buildDataLayerHistoricalAppendix(),
  ]
  return lines.join('\n')
}
