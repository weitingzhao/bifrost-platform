/**
 * Signal-level Agent Fix dispatch — maps failing ProdFixSignal.fixScope
 * to the correct remediation prompt / request body.
 */
import type { ClusterServiceReadinessResponse, ClusterSummary, StgSmokeResponse, SupplyChainResponse } from '@/api/types'
import { buildDeliverStgRecoverPrompt } from '@/lib/agent/deliverStgRecoverPrompt'
import {
  DELIVER_STG_RECOVER_SCOPE,
  PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
} from '@/lib/agent/agentScopes'
import {
  pickFailingFixSignal,
  pickFixScope,
  PROD_ENV_FIX_SCOPE,
  type ProdFixSignal,
} from '@/lib/agent/prodEnvironmentFixPrompt'
import { SATELLITE_BUS_INGEST_TRIAGE_SCOPE } from '@/lib/agent/satelliteBusIngestTriagePrompt'
import { collectClusterIssues } from '@/lib/cluster/collectClusterIssues'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'

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
