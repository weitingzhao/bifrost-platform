import type { Reachability } from './matrixTypes'

export interface SatelliteBusSocketComponent {
  reachability: Reachability
  lamp?: string
  self_check?: string
  detail: string
  raw?: Record<string, unknown>
}

export interface SatelliteBusMonitorHealth {
  self_check?: string
  block_reasons?: string[]
  status_lamp?: string
  reachability: Reachability
}

export interface SatelliteBusMonitorDaemon {
  self_check?: string
  lamp?: string
  block_reasons?: string[]
  trading?: Record<string, unknown>
  heartbeat?: Record<string, unknown>
  auto_status?: Record<string, unknown>
  reachability: Reachability
}

export interface SatelliteBusMonitorSocket {
  massive: SatelliteBusSocketComponent
  ib_ingestor: SatelliteBusSocketComponent
  ib_account_agent: SatelliteBusSocketComponent
  ib_operator: SatelliteBusSocketComponent
  platform_ib_gateway: SatelliteBusSocketComponent
}

export interface SatelliteBusMonitorCelery {
  broker_connected: boolean
  workers: string[]
  worker_ib_connected: boolean
  worker_ib_client_id?: unknown
  worker_last_updated_ts?: unknown
  reachability: Reachability
}

export interface SatelliteBusMonitorAccountSync {
  daemon_alive: boolean
  stream_lag?: unknown
  heartbeat?: Record<string, unknown>
  reachability: Reachability
}

export interface SatelliteBusMonitorDeep {
  reachability: Reachability
  detail: string
  health: SatelliteBusMonitorHealth
  daemon: SatelliteBusMonitorDaemon
  socket: SatelliteBusMonitorSocket
  celery: SatelliteBusMonitorCelery
  account_sync: SatelliteBusMonitorAccountSync
}

export interface SatelliteBusOpsDeep {
  status?: string
  service?: string
  executor_mode?: string
  k8s_reachable?: boolean
  reachability: Reachability
  detail: string
  raw?: Record<string, unknown>
}

export interface SatelliteBusIngestService {
  id: string
  process_active?: string
  runtime_status?: string
  display_active?: string
  runtime_kind?: string
  redis_control_env?: string
  runtime_externally_managed?: boolean
  platform_gateway_managed?: boolean
  reachability: Reachability
  detail: string
}

export interface SatelliteBusIngestDeep {
  services: SatelliteBusIngestService[]
  reachability: Reachability
  detail: string
}

export interface SatelliteBusDeepResponse {
  environment: string
  label: string
  generated_at: string
  reachability: Reachability
  detail: string
  monitor: SatelliteBusMonitorDeep
  ops: SatelliteBusOpsDeep
  ingest: SatelliteBusIngestDeep
}

export interface AllSatelliteBusDeepResponse {
  buses: SatelliteBusDeepResponse[]
}

export type IbGatewayReachability = 'ok' | 'degraded' | 'fail' | 'unknown'

export interface IbGatewaySlotStatus {
  slot: string
  account_id: string
  status: string
  client_id?: number
  connected: boolean
  reachability: IbGatewayReachability
  detail?: string
}

export interface IbGatewayDeploymentStatus {
  namespace: string
  name: string
  ready: string
  mode: string
  reachability: IbGatewayReachability
  detail?: string
}

export interface IbGatewayCutoverEnv {
  namespace: string
  legacy_ib_replicas: number
  redis_ib_external_name_ok: boolean
  reachability: IbGatewayReachability
  detail?: string
}

export interface IbGatewayCutoverStatus {
  legacy_socket_retired: boolean
  reachability: IbGatewayReachability
  environments: IbGatewayCutoverEnv[]
}

export interface IbGatewayStatusResponse {
  reachable?: boolean
  reachability?: IbGatewayReachability
  summary?: string
  mode?: string
  deployment?: IbGatewayDeploymentStatus
  redis_reachability?: IbGatewayReachability
  slots?: IbGatewaySlotStatus[]
  ingestor_health?: Record<string, string>
  account_health?: Record<string, string>
  operator_health?: Record<string, string>
  sample_tick_nvda?: string
  /** Raw JSON from redis-ib `ib:account:snapshot:v1` — used for ghost-session detection. */
  account_snapshot?: string
  operator_consumer_group?: string
  cutover?: IbGatewayCutoverStatus
  autonomy?: string
  error?: string
  hint?: string
  generated_at?: string
}

export interface IbGatewayControlResponse {
  ok: boolean
  action: string
  target: string
  autonomy: string
  message: string
  generated_at?: string
}

export type MarketDataReachability = 'ok' | 'degraded' | 'fail' | 'unknown'

export interface MarketDataDeploymentInfo {
  namespace: string
  name: string
  ready: string
  reachability: MarketDataReachability
  detail?: string
}

export interface MarketDataWorkerInfo {
  pool: string
  status?: string
  jobs_done: number
  jobs_failed: number
  uptime_sec?: number
  last_claim_at?: string
}

export interface MarketDataFreshnessInfo {
  dimension: string
  last_run_at?: string
  rows_written: number
  status?: string
  age_hours: number
  verdict: 'ok' | 'stale' | 'unknown' | string
}

export interface MarketDataStatusResponse {
  reachable?: boolean
  reachability?: MarketDataReachability
  summary?: string
  deployments?: MarketDataDeploymentInfo[]
  workers?: MarketDataWorkerInfo[]
  health_reachability?: MarketDataReachability
  freshness?: MarketDataFreshnessInfo[]
  freshness_reachability?: MarketDataReachability
  autonomy?: string
  error?: string
  hint?: string
  generated_at?: string
}
