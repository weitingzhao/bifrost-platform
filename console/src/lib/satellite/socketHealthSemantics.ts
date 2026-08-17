import type { Reachability } from '@/api/matrixTypes'
import type {
  SatelliteBusDeepResponse,
  SatelliteBusIngestService,
  SatelliteBusMonitorDaemon,
  SatelliteBusSocketComponent,
} from '@/api/satelliteBusTypes'
import { satelliteBusPolygonWs } from '@/api/satelliteBusTypes'
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

/** Matrix columns: K3s dev (cluster pull) + Mac thin-client (satellite-probe-bridge). */
export type BusEnvId = TradeEnv | 'dev-local'

export const SOCKET_TRADE_ENVS: TradeEnv[] = ['dev', 'stg', 'prod']

export const SOCKET_MATRIX_ENVS: BusEnvId[] = ['dev', 'stg', 'prod', 'dev-local']

export const SOCKET_MATRIX_LABELS: Record<BusEnvId, string> = {
  dev: 'K3s Dev',
  stg: 'Stg',
  prod: 'Prod',
  'dev-local': 'Mac',
}

export const SOCKET_TRADE_NS: Record<TradeEnv, string> = {
  dev: 'bifrost-dev',
  stg: 'bifrost-stg',
  prod: 'bifrost-prod',
}

const GENERIC_PROBE_DETAILS = new Set([
  'Parsed monitor schema v9 subset',
  'Deep bus semantics from trade monitor/ops APIs',
  'Deep bus semantics via satellite-probe-bridge',
])

function rawBool(raw: Record<string, unknown> | undefined, key: string): boolean | undefined {
  const v = raw?.[key]
  if (typeof v === 'boolean') return v
  return undefined
}

function rawStr(raw: Record<string, unknown> | undefined, key: string): string {
  const v = raw?.[key]
  return typeof v === 'string' ? v.trim() : ''
}

function policyEnv(env: BusEnvId): TradeEnv {
  return env === 'dev-local' ? 'dev' : env
}

export function formatBusProbeDetail(bus: SatelliteBusDeepResponse): string | undefined {
  const reasons = bus.monitor?.health?.block_reasons ?? []
  if (reasons.length > 0) {
    const lamp = bus.monitor?.health?.status_lamp ?? '?'
    return `health lamp=${lamp}: ${reasons.join(', ')}`
  }
  for (const candidate of [bus.detail, bus.monitor?.detail, bus.ingest?.detail]) {
    if (candidate == null || candidate.trim() === '') continue
    if (GENERIC_PROBE_DETAILS.has(candidate.trim())) continue
    return candidate.trim()
  }
  if (bus.reachability === 'fail' || bus.reachability === 'unknown') {
    return `bus-deep ${bus.reachability}`
  }
  return undefined
}

function ingestById(
  services: SatelliteBusIngestService[] | undefined,
  id: string,
): SatelliteBusIngestService | undefined {
  return services?.find(s => s.id === id)
}

/** Official polygon_ws ingest row only. */
function polygonWsIngest(
  services: SatelliteBusIngestService[] | undefined,
): SatelliteBusIngestService | undefined {
  return ingestById(services, 'polygon_ws')
}

function isPolygonWsConsumerId(id: string): boolean {
  return id === 'polygon_ws'
}

function polygonWsRequired(env: TradeEnv, ingest?: SatelliteBusIngestService, raw?: Record<string, unknown>): SocketRequiredState {
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

/** Platform IB Gateway supplies quotes/account; daemon ``ib_connected`` is the execution-arm rollup (stricter). */
function platformIbDataPathOk(
  socket?: {
    ib_ingestor?: SatelliteBusSocketComponent
    ib_account_agent?: SatelliteBusSocketComponent
    platform_ib_gateway?: SatelliteBusSocketComponent
  },
): boolean {
  if (socket == null) return false
  const consumerUp = (c?: SatelliteBusSocketComponent): boolean => {
    if (c == null) return false
    const raw = c.raw
    if (rawBool(raw, 'connected') === true || rawBool(raw, 'service_alive') === true) return true
    return c.reachability === 'ok'
  }
  if (consumerUp(socket.ib_ingestor) && consumerUp(socket.ib_account_agent)) return true
  const pg = socket.platform_ib_gateway?.raw
  if (pg == null) return false
  const mode = rawStr(pg, 'mode')
  if (mode === 'mock') return true
  return rawBool(pg, 'connected') === true || rawStr(pg, 'lamp') === 'green' || rawStr(pg, 'lamp') === 'yellow'
}

function isIbExecutionArmGapOnly(blockReasons: string[]): boolean {
  return blockReasons.length === 1 && blockReasons[0] === 'ib_not_connected'
}

/** Trading daemon row — uses monitor.daemon + ops ingest + socket bus, not system-wide health rollup. */
export function classifyTradingDaemon(
  env: TradeEnv,
  ingest?: SatelliteBusIngestService,
  daemon?: SatelliteBusMonitorDaemon,
  socket?: {
    ib_ingestor?: SatelliteBusSocketComponent
    ib_account_agent?: SatelliteBusSocketComponent
    platform_ib_gateway?: SatelliteBusSocketComponent
  },
): SocketHealthRow | null {
  if (tradingDaemonPolicyOff(ingest)) {
    return {
      id: 'trading_engine',
      label: 'Trading daemon',
      layer: 'trade',
      required: 'policy-off',
      reach: 'ok',
      reachLabel: 'policy-off',
      detail: `Daemon scaled to 0 by env policy (${env.toUpperCase()})`,
    }
  }

  if (ingest == null && daemon == null) return null

  const heartbeat = daemon?.heartbeat ?? {}
  const daemonAlive = heartbeat.daemon_alive === true
  const trading = daemon?.trading as { trading_suspended?: boolean } | undefined
  const tradingSuspended = trading?.trading_suspended === true
  const blockReasons = daemon?.block_reasons ?? []
  const selfCheck = (daemon?.self_check ?? '').toLowerCase()
  const ingestRuntime = (ingest?.runtime_status ?? '').toLowerCase()
  const ingestDisplay = ingest?.display_active ?? ''
  const ingestInactive =
    (ingest?.process_active ?? '').toLowerCase() === 'inactive' ||
    ingestRuntime === 'inactive' ||
    ingestDisplay.toLowerCase().includes('inactive')

  if (ingestInactive && !daemonAlive) {
    return {
      id: 'trading_engine',
      label: 'Trading daemon',
      layer: 'trade',
      required: 'required',
      reach: 'ok',
      reachLabel: 'stopped',
      detail: ingestDisplay || 'Daemon stopped by operator (not a fault)',
    }
  }

  if (!daemonAlive) {
    const reason = blockReasons[0] ?? 'daemon_not_running'
    return {
      id: 'trading_engine',
      label: 'Trading daemon',
      layer: 'trade',
      required: 'required',
      reach: 'fail',
      reachLabel: 'fail',
      detail: reason === 'heartbeat_stale' ? 'Heartbeat stale — daemon process down or not writing' : reason,
    }
  }

  if (tradingSuspended) {
    return {
      id: 'trading_engine',
      label: 'Trading daemon',
      layer: 'trade',
      required: 'required',
      reach: 'ok',
      reachLabel: 'paused',
      detail: 'Trading suspended by operator (intentional pause — not a fault)',
    }
  }

  if (
    daemonAlive &&
    isIbExecutionArmGapOnly(blockReasons) &&
    platformIbDataPathOk(socket)
  ) {
    const mode = rawStr(socket?.platform_ib_gateway?.raw, 'mode')
    const modeHint = mode === 'mock' ? 'dev mock' : mode === 'live' ? 'live gateway' : 'platform gateway'
    return {
      id: 'trading_engine',
      label: 'Trading daemon',
      layer: 'trade',
      required: 'required',
      reach: 'ok',
      reachLabel: 'observe',
      detail: `Running · observe (${modeHint} feeds data; execution arm not required in ${env.toUpperCase()})`,
    }
  }

  const daemonReach = daemon?.reachability ?? ingest?.reachability ?? 'unknown'
  const faultReasons = blockReasons.filter(r => r !== 'ib_not_connected')
  if (faultReasons.length > 0 || (selfCheck === 'blocked')) {
    const detail = faultReasons.length > 0 ? faultReasons.join(', ') : blockReasons.join(', ')
    return {
      id: 'trading_engine',
      label: 'Trading daemon',
      layer: 'trade',
      required: 'required',
      reach: daemonReach === 'fail' ? 'fail' : 'degraded',
      reachLabel: daemonReach === 'fail' ? 'fail' : 'degraded',
      detail,
    }
  }

  if (selfCheck === 'degraded' || daemonReach === 'degraded') {
    const detail =
      blockReasons.length > 0
        ? blockReasons.join(', ')
        : ingestDisplay || 'Daemon running with degraded checks (e.g. IB not connected in observe/mock)'
    return {
      id: 'trading_engine',
      label: 'Trading daemon',
      layer: 'trade',
      required: 'required',
      reach: 'degraded',
      reachLabel: 'degraded',
      detail,
    }
  }

  if (daemonAlive && (selfCheck === 'ok' || daemonReach === 'ok' || ingestRuntime === 'active')) {
    const auto = daemon?.auto_status as { daemon_state?: string; trading_state?: string } | undefined
    const state = auto?.daemon_state ?? auto?.trading_state
    return {
      id: 'trading_engine',
      label: 'Trading daemon',
      layer: 'trade',
      required: 'required',
      reach: 'ok',
      reachLabel: 'ok',
      detail: state != null ? `Running · ${state}` : ingestDisplay || 'Running',
    }
  }

  return {
    id: 'trading_engine',
    label: 'Trading daemon',
    layer: 'trade',
    required: 'required',
    reach: daemonReach,
    reachLabel: String(daemonReach),
    detail: ingest?.detail ?? (ingestDisplay || '—'),
  }
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
  if (isPolygonWsConsumerId(id)) {
    required = polygonWsRequired(env, ingest, raw)
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
    polygon_ws?: SatelliteBusSocketComponent
    ib_ingestor?: SatelliteBusSocketComponent
    ib_account_agent?: SatelliteBusSocketComponent
    ib_operator?: SatelliteBusSocketComponent
    platform_ib_gateway?: SatelliteBusSocketComponent
  } | undefined,
  env: TradeEnv,
  ingestServices?: SatelliteBusIngestService[],
  daemon?: SatelliteBusMonitorDaemon,
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
    classifyTradeSocketConsumer(
      'polygon_ws',
      'Polygon WS (Plugin)',
      satelliteBusPolygonWs(socket),
      env,
      polygonWsIngest(ingestServices),
    ),
  ]

  const daemonRow = classifyTradingDaemon(env, ingest('trading_engine'), daemon, socket)
  if (daemonRow != null) {
    trade.push(daemonRow)
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
  local: SocketHealthEnvCell
  envDiverges: boolean
}

type BusSocketSlice = {
  socket?: Parameters<typeof buildSocketHealthRows>[0]
  ingest?: SatelliteBusIngestService[]
  daemon?: SatelliteBusMonitorDaemon
  probeDetail?: string
}

function matrixCellKey(env: BusEnvId): keyof Pick<SocketHealthMatrixRow, 'dev' | 'stg' | 'prod' | 'local'> {
  if (env === 'dev-local') return 'local'
  return env
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

/**
 * Compare · DRIFT buckets — intentional observe / D10 scale-zero must not diverge
 * from each other (STG expected-off vs DEV/PROD observe is policy, not drift).
 */
export function matrixDivergeBucket(cell: SocketHealthEnvCell): string | null {
  if (cell.reach === 'unknown' && cell.required !== 'policy-off') return null
  if (cell.required === 'policy-off') return 'healthy-intentional'
  const label = cell.reachLabel.trim().toLowerCase()
  if (
    label === 'observe' ||
    label === 'paused' ||
    label === 'stopped' ||
    label === 'policy-off' ||
    label === 'expected off'
  ) {
    return 'healthy-intentional'
  }
  if (cell.reach === 'ok' && (label === 'ok' || label === 'partial')) {
    return 'healthy-up'
  }
  return `${cell.reach}:${label || cell.reach}`
}

/** True when probed env cells disagree beyond intentional observe / policy-off modes. */
export function computeEnvDiverges(
  cells: Pick<SocketHealthMatrixRow, 'dev' | 'stg' | 'prod' | 'local'>,
): boolean {
  const buckets = new Set<string>()
  for (const env of SOCKET_MATRIX_ENVS) {
    const bucket = matrixDivergeBucket(cells[matrixCellKey(env)])
    if (bucket == null) continue
    buckets.add(bucket)
  }
  return buckets.size > 1
}

const TRADE_CONSUMER_DEFS: { id: string; label: string; includeDaemon?: boolean }[] = [
  { id: 'ib_ingestor', label: 'IB Ingestor' },
  { id: 'ib_account_agent', label: 'IB Account Agent' },
  { id: 'ib_operator', label: 'IB Operator' },
  { id: 'polygon_ws', label: 'Polygon WS (Plugin)' },
  { id: 'trading_engine', label: 'Trading daemon', includeDaemon: true },
]

/** Rocket bus is cluster-shared; pick best reach across envs (not first env). */
export function resolveSharedRocketRow(
  buses: Partial<Record<BusEnvId, BusSocketSlice>>,
): SocketHealthRow {
  let best: SocketHealthRow | null = null
  let bestRank = 0

  for (const env of SOCKET_MATRIX_ENVS) {
    if (env === 'dev-local') continue
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
  buses: Partial<Record<BusEnvId, BusSocketSlice>>,
): { rocket: SocketHealthRow; tradeRows: SocketHealthMatrixRow[] } {
  const rocket = resolveSharedRocketRow(buses)

  const tradeRows: SocketHealthMatrixRow[] = TRADE_CONSUMER_DEFS.map(def => {
    const cells: Record<'dev' | 'stg' | 'prod' | 'local', SocketHealthEnvCell> = {
      dev: emptyCell(),
      stg: emptyCell(),
      prod: emptyCell(),
      local: emptyCell(),
    }

    for (const env of SOCKET_MATRIX_ENVS) {
      const slice = buses[env]
      if (slice == null) continue
      const built = buildSocketHealthRows(slice.socket, policyEnv(env), slice.ingest, slice.daemon)
      const match = built.trade.find(r => r.id === def.id)
      const key = matrixCellKey(env)
      if (match != null) {
        cells[key] = rowToCell(match)
      } else if (def.id !== 'trading_engine' && slice.probeDetail != null) {
        cells[key] = applyProbeDetail(emptyCell(), slice.probeDetail)
      }
    }

    const envDiverges = computeEnvDiverges(cells)

    return {
      id: def.id,
      label: def.label,
      dev: cells.dev,
      stg: cells.stg,
      prod: cells.prod,
      local: cells.local,
      envDiverges,
    }
  }).filter(row => {
    const anyPresent = SOCKET_MATRIX_ENVS.some(
      e => row[matrixCellKey(e)].required !== 'optional' || row[matrixCellKey(e)].reach !== 'unknown',
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

  for (const env of SOCKET_MATRIX_ENVS) {
    const rows: SocketHealthRow[] = [
      matrix.rocket,
      ...matrix.tradeRows.map(r => ({
        id: r.id,
        label: r.label,
        layer: 'trade' as const,
        required: r[matrixCellKey(env)].required,
        reach: r[matrixCellKey(env)].reach,
        reachLabel: r[matrixCellKey(env)].reachLabel,
        detail: r[matrixCellKey(env)].detail,
      })),
    ]
    const summary = summarizeSocketHealth(rows)
    envSignals.push(summary.signal)
    const short = SOCKET_MATRIX_LABELS[env]
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
