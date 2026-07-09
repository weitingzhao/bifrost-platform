import type { Reachability, SatelliteBusIngestService, SatelliteBusSocketComponent } from '@/api/types'
import type { Signal } from '@/lib/control-room/missionSignals'
import { worst } from '@/lib/control-room/missionSignals'

export type SocketHealthLayer = 'rocket' | 'trade'

export type SocketRequiredState = 'required' | 'policy-off' | 'optional'

export type SocketHealthRow = {
  id: string
  label: string
  layer: SocketHealthLayer
  required: SocketRequiredState
  reach: Reachability
  reachLabel: string
  detail: string
}

type TradeEnv = 'dev' | 'stg' | 'prod'

export type TradeEnvId = TradeEnv

export const SOCKET_TRADE_ENVS: TradeEnv[] = ['dev', 'stg', 'prod']

export const SOCKET_TRADE_NS: Record<TradeEnv, string> = {
  dev: 'bifrost-dev',
  stg: 'bifrost-stg',
  prod: 'bifrost-prod',
}

function rawBool(raw: Record<string, unknown> | undefined, key: string): boolean | undefined {
  const v = raw?.[key]
  if (typeof v === 'boolean') return v
  return undefined
}

function rawStr(raw: Record<string, unknown> | undefined, key: string): string {
  const v = raw?.[key]
  return typeof v === 'string' ? v.trim() : ''
}

function ingestById(
  services: SatelliteBusIngestService[] | undefined,
  id: string,
): SatelliteBusIngestService | undefined {
  return services?.find(s => s.id === id)
}

function massiveRequired(env: TradeEnv, ingest?: SatelliteBusIngestService, raw?: Record<string, unknown>): SocketRequiredState {
  const runtime = (ingest?.runtime_status ?? '').toLowerCase()
  const display = (ingest?.display_active ?? '').toLowerCase()
  const wsMode = rawStr(raw, 'ws_mode')
  if (runtime === 'policy-off' || display.includes('ws-disabled') || wsMode === 'rest_only') {
    return 'policy-off'
  }
  if (env === 'stg' && rawBool(raw, 'configured') === false) {
    return 'policy-off'
  }
  return 'required'
}

function tradingDaemonPolicyOff(ingest?: SatelliteBusIngestService): boolean {
  const runtime = (ingest?.runtime_status ?? '').toLowerCase()
  const display = (ingest?.display_active ?? '').toLowerCase()
  return runtime === 'policy-off' || display.includes('daemon scale')
}

/** Classify platform IB gateway aggregate (Rocket socket bus). */
export function classifyPlatformIbGateway(
  component: SatelliteBusSocketComponent | undefined,
): SocketHealthRow {
  const raw = component?.raw
  const lamp = rawStr(raw, 'lamp') || component?.lamp || ''
  const title = rawStr(raw, 'title')
  const connected = rawBool(raw, 'connected')

  const components = raw?.components
  let componentsOk = 0
  let componentsTotal = 0
  if (components != null && typeof components === 'object') {
    for (const block of Object.values(components as Record<string, unknown>)) {
      if (block == null || typeof block !== 'object') continue
      componentsTotal += 1
      const b = block as Record<string, unknown>
      if (b.connected === true || b.service_alive === true) componentsOk += 1
    }
  }

  let reach: Reachability = component?.reachability ?? 'unknown'
  let reachLabel = String(reach)
  let detail = title || component?.detail || 'data/ib-gateway @ redis-ib'

  if (lamp === 'green' || (componentsTotal > 0 && componentsOk === componentsTotal)) {
    reach = 'ok'
    reachLabel = 'ok'
    detail = 'Platform IB Gateway healthy @ redis-ib'
  } else if (componentsOk > 0 && (lamp === 'yellow' || connected === false)) {
    reach = 'degraded'
    reachLabel = 'partial'
    detail = title || `${componentsOk}/${componentsTotal} components live · check TWS slots`
  } else if (componentsOk === 0 && componentsTotal > 0) {
    reach = 'fail'
    reachLabel = 'fail'
    detail = title || 'Platform IB Gateway unreachable @ redis-ib'
  }

  return {
    id: 'platform_ib_gateway',
    label: 'Platform IB Gateway',
    layer: 'rocket',
    required: 'required',
    reach,
    reachLabel,
    detail,
  }
}

function classifyTradeSocketConsumer(
  id: string,
  label: string,
  component: SatelliteBusSocketComponent | undefined,
  env: TradeEnv,
  ingest?: SatelliteBusIngestService,
): SocketHealthRow {
  const raw = component?.raw
  const transport = rawStr(raw, 'transport')
  const connected = rawBool(raw, 'connected')
  const serviceAlive = rawBool(raw, 'service_alive')
  const ingestRuntime = (ingest?.runtime_status ?? '').toLowerCase()

  let required: SocketRequiredState = 'required'
  if (id === 'massive') {
    required = massiveRequired(env, ingest, raw)
  }

  if (required === 'policy-off') {
    return {
      id,
      label,
      layer: 'trade',
      required,
      reach: 'ok',
      reachLabel: 'policy-off',
      detail: 'Not required for this env (REST-only / ws disabled)',
    }
  }

  if (transport === 'platform_gateway' || rawStr(raw, 'health_source') === 'platform_ib_gateway') {
    if (connected === true || serviceAlive === true || ingestRuntime === 'active') {
      return {
        id,
        label,
        layer: 'trade',
        required,
        reach: 'ok',
        reachLabel: 'ok',
        detail: `Consuming Platform IB Gateway · transport=${transport || 'platform_gateway'}`,
      }
    }
    if (ingestRuntime === 'degraded' || ingest?.display_active?.includes('offline')) {
      return {
        id,
        label,
        layer: 'trade',
        required,
        reach: 'degraded',
        reachLabel: 'degraded',
        detail: ingest?.display_active ?? 'Platform gateway consumer offline',
      }
    }
  }

  const reach = component?.reachability ?? 'unknown'
  return {
    id,
    label,
    layer: 'trade',
    required,
    reach,
    reachLabel: String(reach),
    detail: component?.detail ?? '—',
  }
}

export function buildSocketHealthRows(
  socket: {
    massive?: SatelliteBusSocketComponent
    ib_ingestor?: SatelliteBusSocketComponent
    ib_account_agent?: SatelliteBusSocketComponent
    ib_operator?: SatelliteBusSocketComponent
    platform_ib_gateway?: SatelliteBusSocketComponent
  } | undefined,
  env: TradeEnv,
  ingestServices?: SatelliteBusIngestService[],
): { rocket: SocketHealthRow[]; trade: SocketHealthRow[] } {
  const ingest = (sid: string) => ingestById(ingestServices, sid)

  const rocket = [classifyPlatformIbGateway(socket?.platform_ib_gateway)]

  const trade: SocketHealthRow[] = [
    classifyTradeSocketConsumer('ib_ingestor', 'IB Ingestor', socket?.ib_ingestor, env, ingest('ib_ingestor')),
    classifyTradeSocketConsumer(
      'ib_account_agent',
      'IB Account Agent',
      socket?.ib_account_agent,
      env,
      ingest('ib_account_agent'),
    ),
    classifyTradeSocketConsumer('ib_operator', 'IB Operator', socket?.ib_operator, env, ingest('ib_operator')),
    classifyTradeSocketConsumer('massive', 'Massive WS', socket?.massive, env, ingest('massive_ws')),
  ]

  if (tradingDaemonPolicyOff(ingest('trading_engine'))) {
    trade.push({
      id: 'trading_engine',
      label: 'Trading daemon',
      layer: 'trade',
      required: 'policy-off',
      reach: 'ok',
      reachLabel: 'policy-off',
      detail: `Daemon scaled to 0 by env policy (${env.toUpperCase()})`,
    })
  }

  return { rocket, trade }
}

export function summarizeSocketHealth(
  rows: SocketHealthRow[],
): { signal: Signal; headline: string; attention: number } {
  const required = rows.filter(r => r.required === 'required')
  const policyOff = rows.filter(r => r.required === 'policy-off').length
  const attention = required.filter(r => r.reach === 'fail' || r.reach === 'degraded').length
  const ok = required.filter(r => r.reach === 'ok').length

  let signal: Signal = 'ok'
  if (required.some(r => r.reach === 'fail')) signal = 'fail'
  else if (attention > 0) signal = 'degraded'
  else if (required.length === 0) signal = 'unknown'

  const parts: string[] = []
  if (required.length > 0) parts.push(`${ok}/${required.length} required ok`)
  if (policyOff > 0) parts.push(`${policyOff} policy-off`)
  if (attention > 0) parts.push(`${attention} need attention`)

  return {
    signal,
    headline: parts.length > 0 ? parts.join(' · ') : 'No socket rows',
    attention,
  }
}

export type SocketHealthEnvCell = {
  reach: Reachability
  reachLabel: string
  required: SocketRequiredState
  detail: string
}

export type SocketHealthMatrixRow = {
  id: string
  label: string
  dev: SocketHealthEnvCell
  stg: SocketHealthEnvCell
  prod: SocketHealthEnvCell
  envDiverges: boolean
}

type BusSocketSlice = {
  socket?: Parameters<typeof buildSocketHealthRows>[0]
  ingest?: SatelliteBusIngestService[]
  /** When bus-deep probe failed or returned partial data */
  probeDetail?: string
}

function reachRank(reach: Reachability): number {
  switch (reach) {
    case 'ok':
      return 4
    case 'degraded':
      return 3
    case 'fail':
      return 2
    default:
      return 1
  }
}

function applyProbeDetail(cell: SocketHealthEnvCell, probeDetail?: string): SocketHealthEnvCell {
  if (cell.reach !== 'unknown' && cell.detail !== 'Not probed' && cell.detail !== '—') {
    return cell
  }
  if (probeDetail == null || probeDetail.trim() === '') {
    return cell
  }
  return {
    ...cell,
    reach: cell.reach === 'unknown' ? 'fail' : cell.reach,
    reachLabel: cell.reach === 'unknown' ? 'fail' : cell.reachLabel,
    detail: probeDetail,
  }
}

function rowToCell(row: SocketHealthRow): SocketHealthEnvCell {
  return {
    reach: row.reach,
    reachLabel: row.reachLabel,
    required: row.required,
    detail: row.detail,
  }
}

function emptyCell(): SocketHealthEnvCell {
  return { reach: 'unknown', reachLabel: 'unknown', required: 'optional', detail: 'Not probed' }
}

const TRADE_CONSUMER_DEFS: { id: string; label: string; includeDaemon?: boolean }[] = [
  { id: 'ib_ingestor', label: 'IB Ingestor' },
  { id: 'ib_account_agent', label: 'IB Account Agent' },
  { id: 'ib_operator', label: 'IB Operator' },
  { id: 'massive', label: 'Massive WS' },
  { id: 'trading_engine', label: 'Trading daemon', includeDaemon: true },
]

/** Rocket bus is cluster-shared; pick best reach across envs (not first env). */
export function resolveSharedRocketRow(
  buses: Partial<Record<TradeEnv, BusSocketSlice>>,
): SocketHealthRow {
  let best: SocketHealthRow | null = null
  let bestRank = 0

  for (const env of SOCKET_TRADE_ENVS) {
    const gateway = buses[env]?.socket?.platform_ib_gateway
    if (gateway == null) continue
    const row = classifyPlatformIbGateway(gateway)
    const rank = reachRank(row.reach)
    if (rank > bestRank) {
      bestRank = rank
      best = row
    }
  }

  return best ?? classifyPlatformIbGateway(undefined)
}

export function buildSocketHealthMatrix(
  buses: Partial<Record<TradeEnv, BusSocketSlice>>,
): { rocket: SocketHealthRow; tradeRows: SocketHealthMatrixRow[] } {
  const rocket = resolveSharedRocketRow(buses)

  const tradeRows: SocketHealthMatrixRow[] = TRADE_CONSUMER_DEFS.map(def => {
    const cells: Record<TradeEnv, SocketHealthEnvCell> = {
      dev: emptyCell(),
      stg: emptyCell(),
      prod: emptyCell(),
    }

    for (const env of SOCKET_TRADE_ENVS) {
      const slice = buses[env]
      if (slice == null) continue
      const built = buildSocketHealthRows(slice.socket, env, slice.ingest)
      const match = built.trade.find(r => r.id === def.id)
      if (match != null) {
        cells[env] = applyProbeDetail(rowToCell(match), slice.probeDetail)
      } else if (slice.probeDetail != null) {
        cells[env] = applyProbeDetail(emptyCell(), slice.probeDetail)
      }
    }

    const labels = SOCKET_TRADE_ENVS.map(e => cells[e].reachLabel)
    const envDiverges = new Set(labels).size > 1

    return {
      id: def.id,
      label: def.label,
      dev: cells.dev,
      stg: cells.stg,
      prod: cells.prod,
      envDiverges,
    }
  }).filter(row => {
    const anyPresent = SOCKET_TRADE_ENVS.some(
      e => row[e].required !== 'optional' || row[e].reach !== 'unknown',
    )
    return anyPresent
  })

  return { rocket, tradeRows }
}

export function summarizeSocketHealthAllEnvs(
  matrix: { rocket: SocketHealthRow; tradeRows: SocketHealthMatrixRow[] },
): { signal: Signal; headline: string; attention: number } {
  const envParts: string[] = []
  let attention = 0
  const envSignals: Signal[] = []

  for (const env of SOCKET_TRADE_ENVS) {
    const rows: SocketHealthRow[] = [
      matrix.rocket,
      ...matrix.tradeRows.map(r => ({
        id: r.id,
        label: r.label,
        layer: 'trade' as const,
        required: r[env].required,
        reach: r[env].reach,
        reachLabel: r[env].reachLabel,
        detail: r[env].detail,
      })),
    ]
    const summary = summarizeSocketHealth(rows)
    envSignals.push(summary.signal)
    const short = env.toUpperCase()
    if (summary.attention > 0) {
      attention += summary.attention
      envParts.push(`${short} ${summary.headline}`)
    } else {
      envParts.push(`${short} ok`)
    }
  }

  return {
    signal: worst(...envSignals),
    headline: envParts.join(' · '),
    attention,
  }
}
