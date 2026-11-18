export type NetworkPolicyClassification = 'POLICY_NOMINAL' | 'POLICY_DRIFT'

export interface NetworkStatusResponse {
  host?: string
  site?: string
  reachable?: boolean
  controller_version?: string
  auth?: string
  session_user?: string
  integration_key_usable?: boolean
  session_path?: string
  error?: string
  hint?: string
  autonomy?: string
}

export interface NetworkAuditResponse {
  classification?: NetworkPolicyClassification
  auth_mode?: string
  controller_version?: string
  integration_key_usable?: boolean
  zone_binding_gaps?: string[]
  missing_policies?: string[]
  bifrost_policy_count?: number
  expected_policy_count?: number
  error?: string
  hint?: string
  autonomy?: string
}

export interface NetworkDeviceView {
  name?: string
  model?: string
  type?: string
  ip?: string
  mac?: string
  state?: number
  state_label?: string
  adopted?: boolean
  uptime?: number
  version?: string
  rx_bytes?: number
  tx_bytes?: number
  rx_rate?: number
  tx_rate?: number
}

export interface NetworkDevicesResponse {
  count?: number
  devices?: NetworkDeviceView[]
  devices_up?: number
  devices_total?: number
  error?: string
  hint?: string
  autonomy?: string
}

export interface NetworkClientView {
  hostname?: string
  name?: string
  ip?: string
  mac?: string
  network?: string
  is_wired?: boolean
  last_seen?: number
  rx_bytes?: number
  tx_bytes?: number
  rx_rate?: number
  tx_rate?: number
}

export interface NetworkClientsResponse {
  count?: number
  clients?: NetworkClientView[]
  error?: string
  hint?: string
  autonomy?: string
}

export interface NetworkAnomalyAlert {
  rule?: string
  severity?: string
  message?: string
  device?: string
  mac?: string
  streak?: number
  count?: number
  floor?: number
}

export interface NetworkAnomaliesResponse {
  autonomy?: string
  count?: number
  alerts?: NetworkAnomalyAlert[]
  tips?: string[]
  probe_ok?: boolean
  rules?: Record<string, unknown>
  error?: string
  hint?: string
}

export interface NetworkSlaResponse {
  autonomy?: string
  probe_ok?: boolean
  source?: string
  devices_up?: number
  devices_total?: number
  devices_up_fraction?: number
  probe_fail_streak?: number
  summary?: string
  tips?: string[]
  error?: string
  hint?: string
}
