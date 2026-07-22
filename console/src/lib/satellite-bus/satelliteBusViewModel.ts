/**
 * Satellite Bus — pure derived view model.
 *
 * The page subject is the **selected Trade namespace** (Satellite environment).
 * Rocket (Platform IB Gateway) and Ground are shared dependencies of that
 * environment — they feed the required data path but are not the same
 * classification axis as Satellite components.
 *
 * Bus semantics are fixed to HEALTHY / DEGRADED / UNAVAILABLE / UNKNOWN —
 * never GO / NO-GO (that vocabulary belongs to Fleet / Launch).
 *
 * D10 note: `policy-off` / expected scaled-to-zero is **EXPECTED OFF** — a
 * neutral, healthy-compatible state. It is never a failure and never produces
 * a "start the daemon" style remediation suggestion.
 */
import type { Reachability } from '@/api/matrixTypes'
import type { SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'
import {
  buildSocketHealthRows,
  resolveSharedRocketRow,
  SOCKET_MATRIX_LABELS,
  SOCKET_TRADE_NS,
  type BusEnvId,
  type SocketHealthRow,
  type TradeEnvId,
} from '@/lib/satellite/socketHealthSemantics'

export type BusHealth = 'healthy' | 'degraded' | 'unavailable' | 'unknown'

export const BUS_HEALTH_LABELS: Record<BusHealth, string> = {
  healthy: 'HEALTHY',
  degraded: 'DEGRADED',
  unavailable: 'UNAVAILABLE',
  unknown: 'UNKNOWN',
}

/** Node/consumer health — status colors express health only (scope is text). */
export type BusNodeHealth = 'ok' | 'degraded' | 'fail' | 'expected-off' | 'unknown'

export function busHealthToReach(health: BusHealth): Reachability {
  switch (health) {
    case 'healthy':
      return 'ok'
    case 'degraded':
      return 'degraded'
    case 'unavailable':
      return 'fail'
    default:
      return 'unknown'
  }
}

export function busNodeHealthToReach(health: BusNodeHealth): Reachability {
  switch (health) {
    case 'ok':
      return 'ok'
    case 'degraded':
      return 'degraded'
    case 'fail':
      return 'fail'
    case 'expected-off':
      // Neutral — rendered with a muted lamp, never red.
      return 'unknown'
    default:
      return 'unknown'
  }
}

export type BusPathNodeId = 'gateway' | 'redis-ib' | 'consumers' | 'namespace'

export type BusScopeLabel = 'SHARED' | 'ALL ENVS' | 'SELECTED'

export type BusPathNode = {
  id: BusPathNodeId
  label: string
  scopeLabel: BusScopeLabel
  health: BusNodeHealth
  stateLabel: string
  headline: string
  detail: string
  probePath: string
  raw?: unknown
}

export type BusConsumerKind = 'data-path' | 'runtime'

export type BusConsumerRequirement = 'required' | 'optional' | 'expected-off'

export type BusConsumerRow = {
  id: string
  label: string
  kind: BusConsumerKind
  requirement: BusConsumerRequirement
  health: BusNodeHealth
  /** OK | DEGRADED | UNEXPECTED DOWN | EXPECTED OFF | UNKNOWN | observe/paused/stopped… */
  stateLabel: string
  detail: string
  probePath: string
  raw?: unknown
}

export type BusIssueScope = 'selected' | 'shared' | 'cross-env'

export type BusAttentionIssue = {
  id: string
  scope: BusIssueScope
  env: BusEnvId | 'shared'
  envLabel: string
  severity: 'critical' | 'warning'
  title: string
  detail: string
  nodeId?: BusPathNodeId
  probePath: string
  raw?: unknown
}

export type SatelliteBusViewModel = {
  selectedEnv: TradeEnvId
  namespace: string
  health: BusHealth
  healthLabel: string
  /** Single top reason explaining the verdict (blocking hop first). */
  topReason: string
  metrics: {
    requiredOk: number
    requiredTotal: number
    expectedOff: number
    apiOk: number
    apiTotal: number
    runtimeOk: number
    runtimeTotal: number
  }
  path: BusPathNode[]
  dataPathConsumers: BusConsumerRow[]
  runtimeConsumers: BusConsumerRow[]
  /** Selected-env + shared-dependency issues (actionable here). */
  attention: BusAttentionIssue[]
  /** Other environments — never poison the selected verdict. */
  crossEnvIssues: BusAttentionIssue[]
}

/**
 * Inputs for the Bus Health verdict. Intentionally excludes K8s workload
 * readiness: workloads are namespace operational evidence (Evidence layer),
 * not part of the shared IB bus path, and must never affect Bus Health.
 */
export type SatelliteBusViewModelInput = {
  selectedEnv: TradeEnvId
  buses: Partial<Record<BusEnvId, SatelliteBusDeepResponse>>
  /** Matrix L0 counts for the selected env trade APIs. */
  tradeApi: { ok: number; total: number }
}

/* ── helpers ── */

function envLabel(env: BusEnvId): string {
  return SOCKET_MATRIX_LABELS[env] ?? env.toUpperCase()
}

function consumerHealth(row: SocketHealthRow): BusNodeHealth {
  if (row.required === 'policy-off') return 'expected-off'
  switch (row.reach) {
    case 'ok':
      return 'ok'
    case 'degraded':
      return 'degraded'
    case 'fail':
      return 'fail'
    default:
      return 'unknown'
  }
}

function consumerStateLabel(row: SocketHealthRow): string {
  if (row.required === 'policy-off') return 'EXPECTED OFF'
  if (row.reach === 'fail') return row.required === 'required' ? 'UNEXPECTED DOWN' : 'DOWN'
  if (row.reach === 'degraded') return 'DEGRADED'
  if (row.reach === 'unknown') return 'UNKNOWN'
  // ok — surface operator-intent labels (observe / paused / stopped) verbatim.
  return row.reachLabel === 'ok' ? 'OK' : row.reachLabel.toUpperCase()
}

function consumerRequirement(row: SocketHealthRow): BusConsumerRequirement {
  if (row.required === 'policy-off') return 'expected-off'
  return row.required
}

function expectedOffDetail(row: SocketHealthRow): string {
  // Neutral wording — must not read as a fix suggestion (D10 freeze).
  return `${row.detail} — intentional env policy, not a fault`
}

function socketRowToConsumer(
  row: SocketHealthRow,
  kind: BusConsumerKind,
  env: BusEnvId,
  raw?: unknown,
): BusConsumerRow {
  const health = consumerHealth(row)
  return {
    id: row.id,
    label: row.label,
    kind,
    requirement: consumerRequirement(row),
    health,
    stateLabel: consumerStateLabel(row),
    detail: health === 'expected-off' ? expectedOffDetail(row) : row.detail,
    probePath: `bus-deep[${env}].monitor.socket.${row.id === 'trading_engine' ? 'daemon' : row.id}`,
    raw,
  }
}

type EnvRows = { rocket: SocketHealthRow[]; trade: SocketHealthRow[] }

function rowsForEnv(
  buses: SatelliteBusViewModelInput['buses'],
  env: BusEnvId,
): EnvRows | null {
  const bus = buses[env]
  if (bus == null) return null
  return buildSocketHealthRows(
    bus.monitor.socket,
    env === 'dev-local' ? 'dev' : env,
    bus.ingest.services,
    bus.monitor.daemon,
  )
}

const DATA_PATH_IDS = new Set(['ib_ingestor', 'ib_account_agent', 'ib_operator', 'massive'])

function splitTradeRows(rows: SocketHealthRow[]): {
  dataPath: SocketHealthRow[]
  daemon: SocketHealthRow | undefined
} {
  return {
    dataPath: rows.filter(r => DATA_PATH_IDS.has(r.id)),
    daemon: rows.find(r => r.id === 'trading_engine'),
  }
}

function rollupNodeHealth(rows: BusConsumerRow[]): BusNodeHealth {
  const scored = rows.filter(r => r.health !== 'expected-off')
  if (scored.length === 0) return rows.length > 0 ? 'expected-off' : 'unknown'
  if (scored.some(r => r.health === 'fail')) return 'fail'
  if (scored.some(r => r.health === 'degraded')) return 'degraded'
  if (scored.some(r => r.health === 'unknown')) return 'unknown'
  return 'ok'
}

function nodeStateLabel(health: BusNodeHealth): string {
  switch (health) {
    case 'ok':
      return 'OK'
    case 'degraded':
      return 'DEGRADED'
    case 'fail':
      return 'FAIL'
    case 'expected-off':
      return 'EXPECTED OFF'
    default:
      return 'UNKNOWN'
  }
}

/* ── runtime consumer classification (selected env only) ── */

function buildRuntimeConsumers(
  bus: SatelliteBusDeepResponse | undefined,
  env: TradeEnvId,
  daemonRow: SocketHealthRow | undefined,
  tradeApi: { ok: number; total: number },
): BusConsumerRow[] {
  const rows: BusConsumerRow[] = []

  const daemonExpectedOff = daemonRow?.required === 'policy-off'
  if (daemonRow != null) {
    rows.push(socketRowToConsumer(daemonRow, 'runtime', env, bus?.monitor.daemon))
  } else {
    rows.push({
      id: 'trading_engine',
      label: 'Trading daemon',
      kind: 'runtime',
      requirement: 'required',
      health: 'unknown',
      stateLabel: 'UNKNOWN',
      detail: 'No daemon probe for this environment',
      probePath: `bus-deep[${env}].monitor.daemon`,
    })
  }

  const apiHealth: BusNodeHealth =
    tradeApi.total === 0
      ? 'unknown'
      : tradeApi.ok === tradeApi.total
        ? 'ok'
        : tradeApi.ok === 0
          ? 'fail'
          : 'degraded'
  rows.push({
    id: 'trade-apis',
    label: 'Trade APIs',
    kind: 'runtime',
    requirement: 'required',
    health: apiHealth,
    stateLabel:
      apiHealth === 'ok' ? 'OK' : apiHealth === 'fail' ? 'DOWN' : apiHealth === 'degraded' ? 'PARTIAL' : 'UNKNOWN',
    detail: tradeApi.total === 0 ? 'No matrix probes yet' : `${tradeApi.ok}/${tradeApi.total} matrix targets reachable`,
    probePath: `matrix[${env}].targets(trade_api)`,
  })

  const celery = bus?.monitor.celery
  const celeryHealth: BusNodeHealth =
    celery == null
      ? 'unknown'
      : celery.reachability === 'ok' && celery.broker_connected
        ? 'ok'
        : celery.reachability === 'fail'
          ? 'fail'
          : 'degraded'
  rows.push({
    id: 'celery-workers',
    label: 'Celery workers',
    kind: 'runtime',
    requirement: 'required',
    health: celeryHealth,
    stateLabel:
      celeryHealth === 'ok' ? 'OK' : celeryHealth === 'fail' ? 'UNEXPECTED DOWN' : celeryHealth === 'degraded' ? 'DEGRADED' : 'UNKNOWN',
    detail:
      celery == null
        ? 'No celery probe'
        : `broker ${celery.broker_connected ? 'connected' : 'disconnected'} · ${celery.workers.length} worker(s)`,
    probePath: `bus-deep[${env}].monitor.celery`,
    raw: celery,
  })

  const sync = bus?.monitor.account_sync
  let syncHealth: BusNodeHealth
  let syncState: string
  let syncDetail: string
  if (sync == null) {
    syncHealth = 'unknown'
    syncState = 'UNKNOWN'
    syncDetail = 'No account-sync probe'
  } else if (daemonExpectedOff && sync.daemon_alive !== true) {
    // Daemon is intentionally scaled to zero — a quiet account sync is expected.
    syncHealth = 'expected-off'
    syncState = 'EXPECTED OFF'
    syncDetail = 'Account sync idle while daemon is scaled to 0 by env policy — not a fault'
  } else if (sync.reachability === 'ok') {
    syncHealth = 'ok'
    syncState = 'OK'
    syncDetail = `daemon_alive=${String(sync.daemon_alive)}`
  } else if (sync.reachability === 'fail') {
    syncHealth = 'fail'
    syncState = 'UNEXPECTED DOWN'
    syncDetail = `account sync unreachable (daemon_alive=${String(sync.daemon_alive)})`
  } else {
    syncHealth = sync.reachability === 'degraded' ? 'degraded' : 'unknown'
    syncState = syncHealth === 'degraded' ? 'DEGRADED' : 'UNKNOWN'
    syncDetail = `reachability=${sync.reachability}`
  }
  rows.push({
    id: 'account-sync',
    label: 'Account sync',
    kind: 'runtime',
    requirement: daemonExpectedOff ? 'expected-off' : 'required',
    health: syncHealth,
    stateLabel: syncState,
    detail: syncDetail,
    probePath: `bus-deep[${env}].monitor.account_sync`,
    raw: sync,
  })

  return rows
}

/* ── cross-env issues ── */

function collectCrossEnvIssues(
  buses: SatelliteBusViewModelInput['buses'],
  selectedEnv: TradeEnvId,
): BusAttentionIssue[] {
  const issues: BusAttentionIssue[] = []
  const envs: BusEnvId[] = ['dev', 'stg', 'prod', 'dev-local']
  for (const env of envs) {
    if (env === selectedEnv) continue
    const rows = rowsForEnv(buses, env)
    if (rows == null) continue
    const { dataPath, daemon } = splitTradeRows(rows.trade)
    for (const row of [...dataPath, ...(daemon != null ? [daemon] : [])]) {
      if (row.required === 'policy-off') continue
      if (row.reach !== 'fail' && row.reach !== 'degraded') continue
      // Cross-env issues are informational — they never change the selected verdict.
      issues.push({
        id: `${env}:${row.id}`,
        scope: 'cross-env',
        env,
        envLabel: envLabel(env),
        severity: row.reach === 'fail' && row.required === 'required' ? 'critical' : 'warning',
        title: `${envLabel(env)} · ${row.label} ${row.reach === 'fail' ? 'down' : 'degraded'}`,
        detail: row.detail,
        probePath: `bus-deep[${env}].monitor.socket.${row.id === 'trading_engine' ? 'daemon' : row.id}`,
      })
    }
  }
  return issues
}

/* ── main builder ── */

export function buildSatelliteBusViewModel(
  input: SatelliteBusViewModelInput,
): SatelliteBusViewModel {
  const { selectedEnv, buses, tradeApi } = input
  const namespace = SOCKET_TRADE_NS[selectedEnv]
  const selectedBus = buses[selectedEnv]

  /* Shared gateway — best reach across trade envs (cluster-shared resource). */
  const slices = Object.fromEntries(
    (['dev', 'stg', 'prod', 'dev-local'] as const).flatMap(env => {
      const bus = buses[env]
      if (bus == null) return []
      return [
        [env, { socket: bus.monitor.socket, ingest: bus.ingest.services, daemon: bus.monitor.daemon }],
      ]
    }),
  )
  const gatewayRow = resolveSharedRocketRow(slices)
  const gatewayHealth: BusNodeHealth =
    gatewayRow.reach === 'ok'
      ? 'ok'
      : gatewayRow.reach === 'degraded'
        ? 'degraded'
        : gatewayRow.reach === 'fail'
          ? 'fail'
          : 'unknown'

  /* Selected env consumers. */
  const selectedRows = rowsForEnv(buses, selectedEnv)
  const { dataPath, daemon } = splitTradeRows(selectedRows?.trade ?? [])
  const rawSocket = selectedBus?.monitor.socket
  const rawFor = (id: string): unknown => {
    if (rawSocket == null) return undefined
    switch (id) {
      case 'ib_ingestor':
        return rawSocket.ib_ingestor
      case 'ib_account_agent':
        return rawSocket.ib_account_agent
      case 'ib_operator':
        return rawSocket.ib_operator
      case 'massive':
        return rawSocket.massive
      default:
        return undefined
    }
  }
  const dataPathConsumers = dataPath.map(r => socketRowToConsumer(r, 'data-path', selectedEnv, rawFor(r.id)))
  const runtimeConsumers = buildRuntimeConsumers(selectedBus, selectedEnv, daemon, tradeApi)

  const requiredDataPath = dataPathConsumers.filter(r => r.requirement === 'required')
  const requiredOk = requiredDataPath.filter(r => r.health === 'ok').length
  const expectedOff =
    dataPathConsumers.filter(r => r.requirement === 'expected-off').length +
    runtimeConsumers.filter(r => r.requirement === 'expected-off').length
  const scoredRuntime = runtimeConsumers.filter(r => r.requirement !== 'expected-off')
  const runtimeOk = scoredRuntime.filter(r => r.health === 'ok').length

  /* redis-ib node is derived: gateway publishes into redis-ib; consumers read it. */
  let redisHealth: BusNodeHealth
  let redisHeadline: string
  if (selectedBus == null) {
    redisHealth = 'unknown'
    redisHeadline = 'No probe data'
  } else if (gatewayHealth === 'fail') {
    redisHealth = 'unknown'
    redisHeadline = 'Not assessable — upstream gateway down'
  } else if (requiredDataPath.length === 0) {
    redisHealth = dataPathConsumers.length > 0 ? 'expected-off' : 'unknown'
    redisHeadline = dataPathConsumers.length > 0 ? 'No required consumers in this env' : 'No consumer probes'
  } else if (requiredDataPath.some(r => r.health === 'ok')) {
    redisHealth = 'ok'
    redisHeadline = 'Consumers reading redis-ib'
  } else if (requiredDataPath.every(r => r.health === 'fail')) {
    redisHealth = 'fail'
    redisHeadline = 'No consumer can read redis-ib'
  } else if (requiredDataPath.some(r => r.health === 'degraded')) {
    redisHealth = 'degraded'
    redisHeadline = 'Consumers partially reading redis-ib'
  } else {
    redisHealth = 'unknown'
    redisHeadline = 'Consumer probes stale'
  }

  const consumersNodeHealth = rollupNodeHealth(dataPathConsumers)
  const namespaceNodeHealth = rollupNodeHealth(runtimeConsumers)

  const path: BusPathNode[] = [
    {
      id: 'gateway',
      label: 'Platform IB Gateway',
      scopeLabel: 'SHARED',
      health: gatewayHealth,
      stateLabel: nodeStateLabel(gatewayHealth),
      headline: gatewayRow.detail,
      detail: 'Rocket dependency — shared TWS connection bus for all trade namespaces (data/ib-gateway)',
      probePath: 'bus-deep[*].monitor.socket.platform_ib_gateway (best across envs)',
      raw: rawSocket?.platform_ib_gateway,
    },
    {
      id: 'redis-ib',
      label: 'redis-ib',
      scopeLabel: 'SHARED',
      health: redisHealth,
      stateLabel: nodeStateLabel(redisHealth),
      headline: redisHeadline,
      detail: 'Shared IB data Redis (redis-ib.data.svc) — derived from gateway publish + consumer reads',
      probePath: 'derived: gateway status + selected-env consumer connectivity',
    },
    {
      id: 'consumers',
      label: 'Socket consumers',
      scopeLabel: 'ALL ENVS',
      health: consumersNodeHealth,
      stateLabel: nodeStateLabel(consumersNodeHealth),
      headline:
        requiredDataPath.length > 0
          ? `${requiredOk}/${requiredDataPath.length} required ok${expectedOff > 0 ? ` · ${expectedOff} expected off` : ''}`
          : dataPathConsumers.length > 0
            ? 'All consumers expected off'
            : 'No consumer probes',
      detail: `Per-namespace socket consumers — ${selectedEnv.toUpperCase()} column is the selected one`,
      probePath: `bus-deep[${selectedEnv}].monitor.socket.*`,
    },
    {
      id: 'namespace',
      label: namespace,
      scopeLabel: 'SELECTED',
      health: namespaceNodeHealth,
      stateLabel: nodeStateLabel(namespaceNodeHealth),
      headline: `${runtimeOk}/${scoredRuntime.length} monitor consumers ok${expectedOff > 0 ? ` · expected-off neutral` : ''}`,
      detail: `Selected namespace monitor consumers — trading daemon / Trade APIs / Celery / account sync in ${namespace}`,
      probePath: `bus-deep[${selectedEnv}].monitor + matrix[${selectedEnv}]`,
    },
  ]

  /* ── verdict ── */
  let health: BusHealth
  let topReason: string

  const failedRequired = requiredDataPath.filter(r => r.health === 'fail')
  const degradedRequired = requiredDataPath.filter(r => r.health === 'degraded')
  const unknownRequired = requiredDataPath.filter(r => r.health === 'unknown')
  const failedOptional = dataPathConsumers.filter(
    r => r.requirement === 'optional' && (r.health === 'fail' || r.health === 'degraded'),
  )
  const runtimeIssues = scoredRuntime.filter(r => r.health === 'fail' || r.health === 'degraded')

  if (selectedBus == null) {
    health = 'unknown'
    topReason = `No bus probe for ${selectedEnv.toUpperCase()} — bus-deep response missing or stale`
  } else if (gatewayHealth === 'fail') {
    health = 'unavailable'
    topReason = `Shared Platform IB Gateway down — ${gatewayRow.detail}`
  } else if (failedRequired.length > 0) {
    health = 'unavailable'
    topReason = `Required consumer down in ${selectedEnv.toUpperCase()}: ${failedRequired.map(r => r.label).join(', ')}`
  } else if (redisHealth === 'fail') {
    health = 'unavailable'
    topReason = 'redis-ib path broken — gateway up but no consumer can read the bus'
  } else if (gatewayHealth === 'degraded' || degradedRequired.length > 0) {
    health = 'degraded'
    topReason =
      gatewayHealth === 'degraded'
        ? `Shared Platform IB Gateway partial — ${gatewayRow.detail}`
        : `Required consumer degraded: ${degradedRequired.map(r => r.label).join(', ')}`
  } else if (failedOptional.length > 0) {
    health = 'degraded'
    topReason = `Optional consumer needs attention: ${failedOptional.map(r => r.label).join(', ')}`
  } else if (runtimeIssues.length > 0) {
    health = 'degraded'
    topReason = `Monitor consumer needs attention: ${runtimeIssues.map(r => r.label).join(', ')}`
  } else if (unknownRequired.length > 0 || gatewayHealth === 'unknown') {
    health = 'unknown'
    topReason =
      unknownRequired.length > 0
        ? `Required probe missing/stale: ${unknownRequired.map(r => r.label).join(', ')}`
        : 'Shared Platform IB Gateway probe missing/stale'
  } else {
    health = 'healthy'
    topReason =
      expectedOff > 0
        ? `All required bus hops healthy · ${expectedOff} expected off by env policy`
        : 'All required bus hops healthy'
  }

  /* ── attention (selected + shared) ── */
  const attention: BusAttentionIssue[] = []
  if (gatewayHealth === 'fail' || gatewayHealth === 'degraded') {
    attention.push({
      id: 'shared:gateway',
      scope: 'shared',
      env: 'shared',
      envLabel: 'SHARED',
      severity: gatewayHealth === 'fail' ? 'critical' : 'warning',
      title: `Platform IB Gateway ${gatewayHealth === 'fail' ? 'down' : 'partial'} (affects all envs)`,
      detail: gatewayRow.detail,
      nodeId: 'gateway',
      probePath: 'bus-deep[*].monitor.socket.platform_ib_gateway',
      raw: rawSocket?.platform_ib_gateway,
    })
  }
  if (selectedBus == null) {
    attention.push({
      id: `${selectedEnv}:probe-missing`,
      scope: 'selected',
      env: selectedEnv,
      envLabel: envLabel(selectedEnv),
      severity: 'warning',
      title: `No bus probe for ${selectedEnv.toUpperCase()}`,
      detail: 'bus-deep response missing — verdict is UNKNOWN until the probe returns',
      probePath: `bus-deep[${selectedEnv}]`,
    })
  }
  for (const row of dataPathConsumers) {
    if (row.requirement === 'expected-off') continue
    if (row.health !== 'fail' && row.health !== 'degraded' && row.health !== 'unknown') continue
    if (row.health === 'unknown' && selectedBus == null) continue
    attention.push({
      id: `${selectedEnv}:${row.id}`,
      scope: 'selected',
      env: selectedEnv,
      envLabel: envLabel(selectedEnv),
      severity: row.health === 'fail' && row.requirement === 'required' ? 'critical' : 'warning',
      title: `${row.label} ${row.stateLabel}`,
      detail: row.detail,
      nodeId: 'consumers',
      probePath: row.probePath,
      raw: row.raw,
    })
  }
  for (const row of runtimeConsumers) {
    if (row.requirement === 'expected-off' || row.health === 'expected-off') continue
    if (row.health !== 'fail' && row.health !== 'degraded') continue
    attention.push({
      id: `${selectedEnv}:runtime:${row.id}`,
      scope: 'selected',
      env: selectedEnv,
      envLabel: envLabel(selectedEnv),
      severity: 'warning',
      title: `${row.label} ${row.stateLabel}`,
      detail: row.detail,
      nodeId: 'namespace',
      probePath: row.probePath,
      raw: row.raw,
    })
  }

  const crossEnvIssues = collectCrossEnvIssues(buses, selectedEnv)

  return {
    selectedEnv,
    namespace,
    health,
    healthLabel: BUS_HEALTH_LABELS[health],
    topReason,
    metrics: {
      requiredOk,
      requiredTotal: requiredDataPath.length,
      expectedOff,
      apiOk: tradeApi.ok,
      apiTotal: tradeApi.total,
      runtimeOk,
      runtimeTotal: scoredRuntime.length,
    },
    path,
    dataPathConsumers,
    runtimeConsumers,
    attention,
    crossEnvIssues,
  }
}
