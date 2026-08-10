/**
 * Signal-level Agent Fix dispatch — maps failing ProdFixSignal.fixScope
 * to the correct remediation prompt / request body.
 */
import type { ClusterServiceReadinessResponse, ClusterSummary } from '@/api/clusterTypes'
import type { StgSmokeResponse, SupplyChainResponse } from '@/api/deliveryTypes'
import { buildDeliverStgRecoverPrompt } from '@/lib/agent/deliverStgRecoverPrompt'
import {
  DATA_LAYER_BACKUP_SCOPE,
  DELIVER_STG_RECOVER_SCOPE,
  PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
} from '@/lib/agent/agentScopes'
import {
  pickFailingFixSignal,
  pickFixScope,
  PROD_ENV_FIX_SCOPE,
  type ProdFixSignal,
} from '@/lib/agent/prodEnvironmentFixPrompt'
import { OPERATOR_PLANE_FIX_SCOPE } from '@/lib/agent/operatorPlaneFixPrompt'
import { SATELLITE_BUS_INGEST_TRIAGE_SCOPE } from '@/lib/agent/satelliteBusIngestTriagePrompt'
import { collectClusterIssues } from '@/lib/cluster/collectClusterIssues'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'

/** Playbook markers consumed by remediation runner prompt routing (avoid generic fallback). */
export const MASSIVE_FEED_PLAYBOOK = 'Playbook: massive-feed-recover'
export const DATA_LAYER_PLAYBOOK = 'Playbook: data-layer-recover'

export { pickFixScope, pickFailingFixSignal }

export type FixDispatchClusterPack = {
  cluster: ClusterSummary
  serviceReadiness: ClusterServiceReadinessResponse
}

export type FixDispatchExtras = {
  supply?: SupplyChainResponse
  stgSmoke?: StgSmokeResponse
  /** Prefer platform deliver when Rocket / supply chain mentions platform pipeline. */
  pipeline?: 'bifrost-deliver-stg' | 'bifrost-deliver-platform'
}

/** Build agent prompt for a dispatched fixScope (signal-level routing). */
export function buildDispatchedFixPrompt(input: {
  scope: string
  signals: ProdFixSignal[]
  /** Fallback prompt when scope is cluster_issues_full_auto (or unknown). */
  clusterFallbackPrompt: string
  extras?: FixDispatchExtras
  busTriagePrompt?: string
}): string {
  const { scope, signals, clusterFallbackPrompt, extras, busTriagePrompt } = input
  const failing = pickFailingFixSignal(signals)

  if (scope === DELIVER_STG_RECOVER_SCOPE) {
    return buildDeliverStgRecoverPrompt({
      supply: extras?.supply,
      stgSmoke: extras?.stgSmoke,
      pipeline: extras?.pipeline ?? 'bifrost-deliver-stg',
    })
  }

  if (scope === DATA_LAYER_BACKUP_SCOPE) {
    return [
      'Playbook: data-layer-backup',
      '',
      failing != null
        ? `Issue: ${failing.label} (${failing.signal}): ${failing.detail}`
        : 'Issue: CNPG backup older than 48h',
      '',
      '## Backup freshness workflow',
      '1. get_postgres_backup_status',
      '2. If stale / stuck / WAL archive fail: repair_cnpg_wal_store (operator)',
      '3. Re-check get_postgres_backup_status; report Backup CR name + cleared objects',
      '',
      'No DDL / PVC wipe / D10. Do not delete completed Backup CRs.',
    ].join('\n')
  }

  if (scope === PLATFORM_SELF_HEALTH_RECOVER_SCOPE) {
    return [
      `Playbook: platform-self-health-recover`,
      '',
      failing != null
        ? `Issue: ${failing.label} (${failing.signal}): ${failing.detail}`
        : 'Issue: Platform self-health probes failing',
      '',
      '## Self-health workflow',
      '1. verify_mission_snapshot — Control self-health signals.',
      '2. Fix bifrost-platform-prod / bifrost-platform-stg pods and NodePort routes.',
      '3. rollout_restart_deployment with operator approval for platform-api/console when needed.',
      '',
      'Before closing: verify_mission_snapshot + post_fix_verification.passed must be true.',
    ].join('\n')
  }

  if (scope === SATELLITE_BUS_INGEST_TRIAGE_SCOPE) {
    return (
      busTriagePrompt ??
      [
        `Playbook: satellite-bus-ingest-triage`,
        '',
        failing != null
          ? `Issue: ${failing.label} (${failing.signal}): ${failing.detail}`
          : 'Issue: Rocket IB socket / bus ingest degraded',
        '',
        'Cross-check bus-deep ingest vs monitor.socket vs ib-gateway plugin;',
        'classify policy-off/managed-ok/false-alarm; safe L1 restart only (D10 — no daemon scale).',
      ].join('\n')
    )
  }

  if (scope === OPERATOR_PLANE_FIX_SCOPE) {
    return [
      'Playbook: operator-plane-remediate',
      '',
      failing != null
        ? `Issue: ${failing.label} (${failing.signal}): ${failing.detail}`
        : 'Issue: Operator Plane runners / git-bridge degraded',
      '',
      '## Runners HA',
      '1. get_agent_bridge + get_remediation_health',
      '2. peer_agent_health → restart_peer_agent if peer still down after watchdog window',
      '3. request_operator_manual_steps for Mac Pro git-bridge / .env when needed',
      '',
      'D10: observe IB only — no live trade enablement.',
    ].join('\n')
  }

  if (scope === 'git-dirty-remediate') {
    return [
      'Playbook: git-dirty-remediate',
      '',
      failing != null
        ? `Issue: ${failing.label} (${failing.signal}): ${failing.detail}`
        : 'Issue: Git bridge dirty repos',
      '',
      '1. git_workspace_status + git_diff',
      '2. request_operator_approval → git_commit (or git_stash if operator prefers)',
      '3. Re-check git_workspace_status',
      '',
      'Never auto-commit/stash without approval. Never discard Owner WIP. D10: no live trading.',
    ].join('\n')
  }

  if (scope === PROD_ENV_FIX_SCOPE && failing != null) {
    const label = `${failing.label} ${failing.detail}`.toLowerCase()
    if (/massive|polygon/.test(label)) {
      return [
        MASSIVE_FEED_PLAYBOOK,
        '',
        `Issue: ${failing.label} (${failing.signal}): ${failing.detail}`,
        '',
        '1. verify_mission_snapshot + verify_payload for massive targets',
        '2. get_cluster_summary — massive-ws / api-massive pods',
        '3. rollout_restart_deployment with approval; or manual API key rotation',
        '4. verify_mission_snapshot before close',
        '',
        'D10: do not scale daemon or enable live trading.',
      ].join('\n')
    }
    if (/postgres|cnpg|redis|datastore|data layer/.test(label)) {
      return [
        DATA_LAYER_PLAYBOOK,
        '',
        `Issue: ${failing.label} (${failing.signal}): ${failing.detail}`,
        '',
        '1. verify_payload + verify_mission_snapshot (DATA_LAYER vs PROBE_DRIFT)',
        '2. get_cluster_summary — data NS pods/PVCs/CNPG',
        '3. Redis: safe rollout restart; Postgres: approve before primary disruption',
        '4. verify_mission_snapshot before close',
      ].join('\n')
    }
  }

  return clusterFallbackPrompt
}

export function buildClusterPackBody(pack: FixDispatchClusterPack) {
  return {
    cluster_summary: pack.cluster,
    service_readiness: pack.serviceReadiness,
    issues: collectClusterIssues({
      summary: pack.cluster,
      serviceReadiness: pack.serviceReadiness,
    }),
  }
}

/**
 * Daily-ops: derive synthetic fixSignals from mission snapshot so Agent Fix
 * can dispatch to deliver-stg-recover / self-health / cluster remediate.
 */
export function missionSnapshotToFixSignals(snap: MissionSnapshot): ProdFixSignal[] {
  return [
    {
      label: 'Release / supply chain',
      signal: snap.release.signal,
      detail: snap.release.detail,
      fixScope: DELIVER_STG_RECOVER_SCOPE,
    },
    {
      label: 'Control / self-health',
      signal: snap.control.signal,
      detail: snap.control.detail,
      fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
    },
    {
      label: 'Cluster · infra',
      signal: snap.infra.signal,
      detail: snap.infra.detail,
      fixScope: PROD_ENV_FIX_SCOPE,
    },
    {
      label: 'Trade · STG',
      signal: snap.tradeStg.signal,
      detail: snap.tradeStg.detail,
      fixScope: PROD_ENV_FIX_SCOPE,
    },
    {
      label: 'Trade · PROD',
      signal: snap.tradeProd.signal,
      detail: snap.tradeProd.detail,
      fixScope: PROD_ENV_FIX_SCOPE,
    },
  ]
}

export function fixScopeAgentTitle(_scope: string, scopeLabel: string, signalLabel?: string): string {
  const focus = signalLabel != null && signalLabel !== '' ? ` · ${signalLabel}` : ''
  return `${scopeLabel}${focus} — fix primary blocking readiness signal`
}
