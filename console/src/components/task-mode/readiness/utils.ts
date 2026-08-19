import type { ClusterSummary } from '@/api/clusterTypes'
import { isAllSatelliteBusDeep } from '@/api/core'
import type { ReleaseGateResponse } from '@/api/deliveryTypes'
import type {
  AllSatelliteBusDeepResponse,
  SatelliteBusDeepResponse,
  SatelliteBusSocketComponent,
} from '@/api/satelliteBusTypes'
import type { SelfHealthProbe } from '@/api/matrixTypes'
import type { MatrixResponse, Reachability } from '@/api/matrixTypes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import {
  missionStatus,
  worst,
  type ModuleState,
  type Signal,
} from '@/lib/control-room/missionSignals'
import { classifyPlatformIbGateway, classifyTradingDaemon } from '@/lib/satellite/socketHealthSemantics'

export const REFETCH_MS = 20_000
export const STG_NS = 'bifrost-stg'
export const PROD_NS = 'bifrost-prod'
export const PLATFORM_STG = 'bifrost-platform-stg'
export const PLATFORM_PROD = 'bifrost-platform-prod'

export type EnvChip = { label: string; signal: Signal; detail: string; fixScope?: string }

export function stripOverallTag(signal: Signal, isLoading: boolean) {
  if (isLoading) return { variant: 'category' as const, label: 'Probing…' }
  const status = missionStatus(signal)
  return {
    variant: (status === 'NOMINAL' ? 'success' : status === 'CRITICAL' ? 'danger' : 'warning') as
      | 'success'
      | 'danger'
      | 'warning',
    label: status,
  }
}

export function findMatrixTarget(matrices: MatrixResponse[], env: string, targetId: string) {
  return matrices.find(m => m.environment === env)?.targets.find(t => t.id === targetId)
}

export function datastoreEnvSignal(matrices: MatrixResponse[], env: string): Signal {
  const pg = findMatrixTarget(matrices, env, 'postgres')
  const redis = findMatrixTarget(matrices, env, 'redis')
  const signals = [pg?.reachability, redis?.reachability].filter(Boolean) as Reachability[]
  if (signals.length === 0) return 'unknown'
  return worst(...signals.map(r => r as Signal))
}

export function datastoreDetail(matrices: MatrixResponse[], env: string): string {
  const pg = findMatrixTarget(matrices, env, 'postgres')
  const redis = findMatrixTarget(matrices, env, 'redis')
  const parts: string[] = []
  if (pg != null) parts.push(`PG ${pg.reachability}`)
  if (redis != null) parts.push(`Redis ${redis.reachability}`)
  return parts.length > 0 ? parts.join(' · ') : 'probing'
}

export function tradeApiSummary(matrix: MatrixResponse | undefined): { signal: Signal; detail: string } {
  if (matrix == null) return { signal: 'unknown', detail: 'probing' }
  const tradeTargets = matrix.targets.filter(
    t =>
      t.category === 'trade_api' ||
      t.category === 'trade_frontend' ||
      t.id === 'nginx-spa' ||
      t.id.startsWith('api-'),
  )
  const total = tradeTargets.length
  if (total === 0) return { signal: 'unknown', detail: 'no API targets' }
  const ok = tradeTargets.filter(t => t.reachability === 'ok').length
  const signal: Signal = ok === total ? 'ok' : ok === 0 ? 'fail' : 'degraded'
  return { signal, detail: `${ok}/${total} APIs reachable` }
}

export function namespacePods(cluster: ClusterSummary | undefined, ns: string): ModuleState {
  if (cluster == null) return { signal: 'unknown', value: '…', detail: `${ns}: probing` }
  const failing = (cluster.failing_pod_details ?? []).filter(p => p.namespace === ns)
  if (failing.length > 0) {
    return {
      signal: 'degraded',
      value: `${failing.length} failing`,
      detail: `${ns}: ${failing.length} failing pod${failing.length === 1 ? '' : 's'}`,
    }
  }
  const clusterFail = cluster.failing_pods
  if (cluster.reachability === 'fail') {
    return { signal: 'fail', value: 'down', detail: 'Cluster API unreachable' }
  }
  return {
    signal: cluster.reachability === 'degraded' ? 'degraded' : 'ok',
    value: ns,
    detail:
      clusterFail > 0
        ? `${ns} OK · ${clusterFail} failing elsewhere`
        : `${ns} workloads nominal`,
  }
}

/**
 * Shared Rocket IB bus — monitor.socket only (no ingest.reachability).
 * Gateway uses classifyPlatformIbGateway so observe/partial ≠ silent fail.
 */
export function sharedRocketFromSocket(socket: {
  ib_ingestor?: SatelliteBusSocketComponent
  ib_account_agent?: SatelliteBusSocketComponent
  ib_operator?: SatelliteBusSocketComponent
  platform_ib_gateway?: SatelliteBusSocketComponent
} | undefined): { signal: Signal; detail: string } {
  if (socket == null) return { signal: 'unknown', detail: 'probing' }
  const gateway = classifyPlatformIbGateway(socket.platform_ib_gateway)
  const reaches: Signal[] = [
    (socket.ib_ingestor?.reachability ?? 'unknown') as Signal,
    (socket.ib_account_agent?.reachability ?? 'unknown') as Signal,
    (socket.ib_operator?.reachability ?? 'unknown') as Signal,
    gateway.reach as Signal,
  ]
  const signal = worst(...reaches)
  const ok = reaches.filter(r => r === 'ok').length
  const gwHint = gateway.reach !== 'ok' ? ` · gateway ${gateway.reachLabel}` : ''
  return { signal, detail: `${ok}/4 socket OK${gwHint}` }
}

/**
 * Account sync readiness chip — aligns with Satellite Bus PAIR ASYMMETRIC.
 * Label must stay "Account sync" so readinessChipFixActions can actuate restart.
 */
export function accountSyncChipFromBus(
  bus: SatelliteBusDeepResponse | undefined,
  env: 'stg' | 'prod' | 'dev',
): EnvChip {
  const base = {
    label: 'Account sync',
    fixScope: PROD_ENV_FIX_SCOPE,
  } as const

  if (bus == null) {
    return { ...base, signal: 'unknown', detail: 'probing' }
  }

  const ingest = bus.ingest?.services?.find(s => s.id === 'trading_engine')
  const daemonRow = classifyTradingDaemon(env, ingest, bus.monitor.daemon, bus.monitor.socket)
  const daemonExpectedOff = daemonRow?.required === 'policy-off'
  const daemonTradingSideUp =
    daemonRow != null &&
    daemonRow.required !== 'policy-off' &&
    (daemonRow.reach === 'ok' || daemonRow.reach === 'degraded')

  const sync = bus.monitor.account_sync

  if (daemonExpectedOff && (sync == null || sync.daemon_alive !== true)) {
    return {
      ...base,
      signal: 'ok',
      detail: 'expected off (daemon scale 0) — not a fault',
    }
  }

  if (sync == null) {
    if (daemonTradingSideUp) {
      return {
        ...base,
        signal: 'degraded',
        detail: 'pair asymmetric · daemon up · account-sync down (co-scale pair)',
      }
    }
    return { ...base, signal: 'unknown', detail: 'no account-sync probe' }
  }

  if (
    daemonTradingSideUp &&
    (sync.daemon_alive !== true ||
      sync.reachability === 'fail' ||
      sync.reachability === 'degraded' ||
      sync.reachability === 'unknown')
  ) {
    return {
      ...base,
      signal: 'degraded',
      detail: 'pair asymmetric · daemon up · account-sync down (co-scale pair)',
    }
  }

  if (sync.reachability === 'ok') {
    return {
      ...base,
      signal: 'ok',
      detail: `daemon_alive=${String(sync.daemon_alive)}`,
    }
  }

  if (sync.reachability === 'fail') {
    return {
      ...base,
      signal: 'fail',
      detail: `account sync unreachable (daemon_alive=${String(sync.daemon_alive)})`,
    }
  }

  return {
    ...base,
    signal: sync.reachability === 'degraded' ? 'degraded' : 'unknown',
    detail: `reachability=${sync.reachability}`,
  }
}

export function releaseGateSignal(gate: ReleaseGateResponse | undefined): { signal: Signal; detail: string } {
  if (gate == null) return { signal: 'unknown', detail: 'probing' }
  if (gate.result === 'pass') return { signal: 'ok', detail: 'Gate passed' }
  if (gate.result === 'fail') return { signal: 'fail', detail: gate.detail?.trim() || 'Gate failed' }
  return { signal: 'degraded', detail: 'Gate not run' }
}

export function selfHealthEnvSignal(
  probes: SelfHealthProbe[] | undefined,
  env: 'stg' | 'prod',
): { signal: Signal; detail: string } {
  const filtered = probes?.filter(p => p.env === env) ?? []
  if (filtered.length === 0) return { signal: 'unknown', detail: 'probing' }
  const ok = filtered.filter(p => p.status === 'ok').length
  const signal = worst(...filtered.map(p => p.status as Signal))
  return { signal, detail: `${ok}/${filtered.length} probes OK (${env})` }
}

export function isProdReleaseBlocked(signal: Signal): boolean {
  return signal === 'fail' || signal === 'degraded'
}

/**
 * Live Platform Prod for Rocket launch GO / NO-GO.
 * Last Tekton deliver (`snapshot.release`) stays in cycle history and the Pipeline
 * idle checkpoint — folding it in made a stale/failed cycle look like Platform Prod
 * CAUTION and a fifth "Fix Prod" row next to `N/4 ready`.
 */
export function rocketLaunchProdOverall(input: {
  k8s: Signal
  selfHealth: Signal
  prodGate: Signal
}): Signal {
  return worst(input.k8s, input.selfHealth, input.prodGate)
}

export function busForEnv(
  data: SatelliteBusDeepResponse | AllSatelliteBusDeepResponse | undefined,
  env: 'stg' | 'prod',
): SatelliteBusDeepResponse | undefined {
  if (data == null) return undefined
  if (isAllSatelliteBusDeep(data)) return data.buses.find(b => b.environment === env)
  return data
}

/** Prefer Account sync when selecting FixBar primary chip so pair-asymmetric gets restart action. */
export function pickPrimaryFailingChip<T extends { label: string; signal: Signal }>(
  chips: T[],
): T | undefined {
  const failing = chips.filter(c => c.signal !== 'ok')
  if (failing.length === 0) return undefined
  return (
    failing.find(c => {
      const label = c.label.toLowerCase()
      return label.includes('account sync') || label.includes('pair asymmetric')
    }) ?? failing[0]
  )
}
