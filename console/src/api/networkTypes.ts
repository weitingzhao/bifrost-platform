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

export interface NetworkFirewallApplyResponse {
  action?: string
  target?: string
  autonomy?: string
  include_default_deny?: boolean
  message?: string
  result?: Record<string, unknown>
  post_apply_audit?: NetworkAuditResponse
  error?: string
}

export interface NetworkDeviceView {
  name?: string
  model?: string
  type?: string
  ip?: string
  mac?: string
  state?: number
  adopted?: boolean
  version?: string
}

export interface NetworkDevicesResponse {
  count?: number
  devices?: NetworkDeviceView[]
  error?: string
  hint?: string
}

export interface NetworkClientView {
  hostname?: string
  name?: string
  ip?: string
  mac?: string
  network?: string
  is_wired?: boolean
  last_seen?: number
}

export interface NetworkClientsResponse {
  count?: number
  clients?: NetworkClientView[]
  error?: string
  hint?: string
}

export interface NetworkZonesResponse {
  zones?: Record<string, unknown>[]
  count?: number
  error?: string
  hint?: string
}

export interface NetworkPoliciesResponse {
  policies?: Record<string, unknown>[]
  bifrost_count?: number
  bifrost_policies?: Record<string, unknown>[]
  error?: string
  hint?: string
}
