#!/usr/bin/env node
/**
 * Live cluster failure Top-N triage — reads platform-api and prints markdown report.
 *
 * Usage:
 *   PLATFORM_API=http://127.0.0.1:8780 npx tsx scripts/cluster-failure-triage.ts
 *   npx tsx scripts/cluster-failure-triage.ts --json
 */
import { buildClusterFailureTriage, formatClusterFailureTriageMarkdown } from '../src/lib/cluster/clusterFailureTriage'
import { buildMissionSnapshot } from '../src/lib/control-room/missionSignals'
import type {
  AllMatricesResponse,
  AgentBridgeResponse,
  ClusterPostgresStatusResponse,
  ClusterServiceReadinessResponse,
  ClusterSummary,
  RemediationHealthResponse,
  RetrospectiveReport,
  SelfHealthResponse,
  StgSmokeResponse,
  SupplyChainResponse,
} from '../src/api/types'

const API_BASE = (process.env.PLATFORM_API ?? 'http://127.0.0.1:8780').replace(/\/$/, '')
const jsonOut = process.argv.includes('--json')

async function fetchJson<T>(path: string): Promise<T | null> {
  try {
    const r = await fetch(`${API_BASE}/api/v1${path}`)
    if (!r.ok) {
      console.error(`WARN ${path}: HTTP ${r.status}`)
      return null
    }
    return (await r.json()) as T
  } catch (err) {
    console.error(`WARN ${path}: ${err instanceof Error ? err.message : String(err)}`)
    return null
  }
}

async function main() {
  const [
    summary,
    serviceReadiness,
    postgresStatus,
    matricesResp,
    supply,
    stgSmoke,
    selfHealth,
    bridge,
    runnerHealth,
    retro,
  ] = await Promise.all([
    fetchJson<ClusterSummary>('/cluster'),
    fetchJson<ClusterServiceReadinessResponse>('/cluster/service-readiness'),
    fetchJson<ClusterPostgresStatusResponse>('/cluster/postgres'),
    fetchJson<AllMatricesResponse>('/matrix'),
    fetchJson<SupplyChainResponse>('/delivery/supply-chain'),
    fetchJson<StgSmokeResponse>('/delivery/stg/smoke'),
    fetchJson<SelfHealthResponse>('/self-health'),
    fetchJson<AgentBridgeResponse>('/agent/bridge'),
    fetchJson<RemediationHealthResponse>('/remediation/health'),
    fetchJson<RetrospectiveReport>('/agent/retrospective/report'),
  ])

  if (summary == null) {
    console.error(`Cannot reach platform-api at ${API_BASE}. Start with: make start`)
    process.exit(1)
  }

  const matrices = matricesResp?.matrices ?? []
  const missionSnapshot =
    summary != null
      ? buildMissionSnapshot({
          cluster: summary,
          supply: supply ?? undefined,
          stg: stgSmoke ?? undefined,
          self: selfHealth ?? undefined,
          runner: runnerHealth ?? undefined,
          bridge: bridge ?? undefined,
          matrices,
        })
      : undefined

  const rows = buildClusterFailureTriage({
    summary,
    serviceReadiness: serviceReadiness ?? undefined,
    postgresStatus: postgresStatus ?? undefined,
    missionSnapshot,
    supplyChain: supply ?? undefined,
    stgSmoke: stgSmoke ?? undefined,
    matrices,
    retrospectivePatterns: retro?.patterns ?? [],
    topN: 12,
  })

  if (jsonOut) {
    console.log(JSON.stringify({ generated_at: new Date().toISOString(), api: API_BASE, rows }, null, 2))
    return
  }

  console.log(formatClusterFailureTriageMarkdown(rows, new Date().toISOString()))
  console.log('')
  console.log(`Cluster: ${summary.nodes_ready}/${summary.nodes_total} nodes ready` +
    (summary.elastic_standby ? ` (+${summary.elastic_standby} elastic standby)` : '') +
    (summary.elastic_degraded ? ` · ${summary.elastic_degraded} elastic degraded` : '') +
    ` · ${summary.failing_pods} failing pods`)
  console.log(`API: ${API_BASE}`)
}

main().catch(err => {
  console.error(err)
  process.exit(1)
})
