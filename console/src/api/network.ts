import type {
  IbGatewayControlResponse,
  IbGatewayStatusResponse,
  MarketDataStatusResponse,
} from './satelliteBusTypes'
import type { NetworkAuditResponse, NetworkClientsResponse, NetworkDevicesResponse, NetworkFirewallApplyResponse, NetworkPoliciesResponse, NetworkStatusResponse, NetworkZonesResponse } from './networkTypes'
import { authedFetch } from './client'

async function fetchNetworkJson<T>(path: string): Promise<T> {
  const r = await fetch(path)
  const body = (await r.json()) as T
  if (!r.ok) {
    return {
      ...(body as object),
      error: (body as { error?: string }).error ?? `HTTP ${r.status}`,
    } as T
  }
  return body
}

export async function fetchNetworkStatus(): Promise<NetworkStatusResponse> {
  const r = await fetch('/api/v1/network/status')
  const body = (await r.json()) as NetworkStatusResponse
  if (!r.ok) {
    return {
      ...body,
      reachable: false,
      error: body.error ?? `HTTP ${r.status}`,
    }
  }
  return body
}

export async function fetchNetworkAudit(): Promise<NetworkAuditResponse> {
  const r = await fetch('/api/v1/network/audit')
  const body = (await r.json()) as NetworkAuditResponse
  if (!r.ok) {
    return {
      ...body,
      error: body.error ?? `HTTP ${r.status}`,
    }
  }
  return body
}

export async function fetchNetworkDevices(): Promise<NetworkDevicesResponse> {
  return fetchNetworkJson('/api/v1/network/devices')
}

export async function fetchNetworkClients(): Promise<NetworkClientsResponse> {
  return fetchNetworkJson('/api/v1/network/clients')
}

export async function fetchNetworkZones(): Promise<NetworkZonesResponse> {
  return fetchNetworkJson('/api/v1/network/zones')
}

export async function fetchNetworkPolicies(): Promise<NetworkPoliciesResponse> {
  return fetchNetworkJson('/api/v1/network/policies')
}

export async function fetchIbGatewayStatus(): Promise<IbGatewayStatusResponse> {
  const r = await fetch('/api/v1/plugins/ib-gateway/status')
  const body = (await r.json()) as IbGatewayStatusResponse
  if (!r.ok) {
    return {
      ...body,
      reachable: false,
      error: body.error ?? `HTTP ${r.status}`,
    }
  }
  return body
}

export async function fetchMarketDataStatus(): Promise<MarketDataStatusResponse> {
  const r = await fetch('/api/v1/plugins/market-data/status')
  const body = (await r.json()) as MarketDataStatusResponse
  if (!r.ok) {
    return {
      ...body,
      reachable: false,
      error: body.error ?? `HTTP ${r.status}`,
    }
  }
  return body
}

export async function postIbGatewayControl(
  action: 'reconnect' | 'maintenance' | 'mode',
  body: { account_id?: string; enabled?: boolean; mode?: 'mock' | 'live' } = {},
): Promise<IbGatewayControlResponse> {
  const r = await authedFetch(`ib-gateway ${action}`, `/api/v1/plugins/ib-gateway/control/${action}`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<IbGatewayControlResponse>
}

export async function postNetworkFirewallApply(body: {
  include_default_deny?: boolean
} = {}): Promise<NetworkFirewallApplyResponse> {
  const r = await authedFetch('network firewall apply', '/api/v1/network/firewall/apply', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<NetworkFirewallApplyResponse>
}

