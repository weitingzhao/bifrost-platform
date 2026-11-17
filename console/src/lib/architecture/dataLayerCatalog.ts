/**
 * Data Layer catalog — Redis, PostgreSQL, MinIO architecture principles for K3s (catalog-only).
 *
 * Complements k3sArchitectureCatalog.ts (topology/CI/CD) with stateful-service design.
 * Aligned with Vision § Redis Ideal Topology.
 *
 * Live state (not this catalog):
 * - PG/Redis/MinIO readiness: Rocket → Cluster (Postgres / Redis / Issues panels)
 * - Migrate progress: spine stream data-layer-k3s (GET /api/v1/context) + spine projection
 */

import type { OpsContextResponse } from '@/api/opsContextTypes'
import { projectWaveStatus } from '@/lib/architecture/waveProjection'
import { GENERATED_DATA_LAYER_PHASES } from './migrateWaves.generated'

export const DATA_LAYER_MIGRATION_PHASES = GENERATED_DATA_LAYER_PHASES

export const DATA_LAYER_SOURCE = 'config/migrate-waves/data-layer-k3s.yaml'

export const DATA_LAYER_MIGRATE_STREAM_ID = 'data-layer-k3s'

// ---------------------------------------------------------------------------
// Redis architecture
// ---------------------------------------------------------------------------

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
  {
    dimension: 'Data Services',
    principle: 'Platform API freshness + clone (not a separate plugin repo)',
    note:
      'GET /api/v1/cluster/data-freshness (verdict = lag_vs_prod_days for non-prod; bifrost_prod = reference) · POST /api/v1/cluster/data-clone (admin + confirm:true + confirmation_token=CLONE-FROM-PROD; 409 if clone running) · schedule default disabled · Console: Rocket → Cluster → Postgres panel',
  },
]

// ---------------------------------------------------------------------------
// Data Services — Agent orchestration (Wave 4)
// ---------------------------------------------------------------------------

export const DATA_SERVICES_AGENT_FLOW: string[] = [
  '1. get_data_freshness — observe bifrost_dev / bifrost_stg lag_vs_prod_days vs bifrost_prod (verdict+badge: <3d fresh/green · 3–7d aging/yellow · ≥7d stale/red; prod=reference)',
  '2. Inner-loop cadence (Program trade-dev-inner-loop / D-IL2): also watch last_clone_at — prefer ≤7d even when lag_vs_prod=0 (wall_age can look old on both sides while lag badge stays fresh)',
  '3. If aging or stale (lag ≥ 3d) OR last_clone_at >7d before ledger-heavy UI work, and Owner/ops intent allows: trigger_data_clone with confirmation_token=CLONE-FROM-PROD and confirm=true (prefer full + targets=["bifrost_dev"])',
  '4. Poll get_data_clone_status until status=done|failed (steps: dumping → restoring → verifying); concurrent clone → HTTP 409',
  '5. Optional: rollout_restart Trade API in bifrost-dev (make bounce-dev-apis-after-clone or MCP rollout_restart_deployment) if apps held stale connections',
  '6. Never write bifrost_prod; never dump redis-live-prod → redis-dev (Live = redis-ib, D-IL3); never kubectl from Agent — Platform API is the executor',
  '7. Cursor MCP: after adding tools, reload the bifrost-platform MCP server (Settings → MCP) so get_data_freshness / trigger_data_clone / get_data_clone_status appear',
]

/** Freshness note — stale non-prod DBs surface in Console Postgres panel; Agent uses get_data_freshness. */
export const DATA_FRESHNESS_NOTE =
  'Data freshness: Rocket → Cluster → Postgres → Data Freshness, or Ops TCC → Refresh DEV ledger (Agent Task data-layer-clone, bifrost_dev only). Verdict uses lag_vs_prod_days (fresh|aging|stale; not wall-clock age alone — wall_age in detail can be large while lag=0). Inner-loop cadence (Program trade-dev-inner-loop / D-IL2): prefer last_clone_at ≤7d (or on-demand) before ledger-heavy Trade UI work. If bifrost_dev/stg lag ≥ 7d (stale) or last_clone_at old, Sync from Prod (admin, both targets) or TCC Refresh DEV ledger / MCP trigger_data_clone (confirm:true; targets=["bifrost_dev"]). Aging (3–7d) is a soft warning. Live quotes ≠ PG — use redis-ib (never clone redis-live-prod → redis-dev).'

/**
 * Controlled E2E checklist (Owner-run). Agent may assist observing job/logs but must not
 * DROP/TRUNCATE without explicit Owner confirmation in the maintenance window.
 */
export const DATA_SERVICES_E2E_ACCEPTANCE: string[] = [
  'Baseline: GET /api/v1/cluster/data-freshness — record lag_vs_prod_days + last_clone_at (may be null)',
  'Owner window: admin Sync from Prod (prefer Full with targets=["bifrost_dev"] first to reduce risk; or Console Full dual-target after confirm)',
  'Poll GET /api/v1/cluster/data-clone/{id} until done|failed; on failure confirm remote dump path cleaned (rm -f)',
  'After done: freshness last_clone_at is non-null; lag still reasonable vs prod activity',
  'Invalid selective table name → HTTP 400; concurrent clone → HTTP 409',
  'Weekly auto-clone remains disabled by default — enable only on explicit Owner request',
]

// ---------------------------------------------------------------------------
// K3s data layer migration (spine stream data-layer-k3s)
// ---------------------------------------------------------------------------

export const DATA_LAYER_SESSION_CONSTRAINTS: string[] = [
  'PG hot storage: local-path on postgres node (ubt-k3s-02) — NOT nfs-hot for PGDATA',
  'NAS nfs-hot / nfs-cold: WAL/RDB backups and cold archive only (Retain reclaim)',
  'R-DV1: bifrost_dev / bifrost_stg / bifrost_prod — separate Redis instances per env (legacy options_db retired)',
  'Single-variable: complete stg cutover before prod PG migration',
  'Prod PG cutover requires Owner maintenance window — no parallel compose→k3s changes',
  'Remove per-namespace postgres/redis Deployments from bifrost-{dev,stg,prod} after each env cutover',
]

/** Spine appendix — phased migration queue aligned with spine stream data-layer-k3s. */
export function formatDataLayerSpineAppendix(ctx?: OpsContextResponse): string {
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
  lines.push('')
  lines.push('### Data Services (freshness / clone)')
  lines.push(`- ${DATA_FRESHNESS_NOTE}`)
  for (const step of DATA_SERVICES_AGENT_FLOW) lines.push(`- ${step}`)
  lines.push('')
  lines.push('### Data Services E2E (Owner-controlled)')
  for (const step of DATA_SERVICES_E2E_ACCEPTANCE) lines.push(`- ${step}`)
  return lines.join('\n')
}
