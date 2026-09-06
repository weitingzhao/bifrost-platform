/**
 * GET /market/capabilities — what the current Massive subscriptions cover.
 * Read-only; bare fetch through the platform-api proxy like the other GETs.
 */
export type CapabilityStatus = 'entitled' | 'planned' | 'unavailable'

export type SubscriptionInfo = {
  id: string
  label: string
  window: string
  calls: string
  delay: string | null
}

export type CapabilityInfo = {
  id: string
  label: string
  status: CapabilityStatus
  subscription?: string | null
  requires?: string | null
  used_by?: string[]
  note?: string
}

export type RetiredSlotInfo = {
  slot: string
  capability: string
  requires: string
  reason: string
}

export type CapabilityMatrix = {
  ok: boolean
  policy: string
  subscriptions: SubscriptionInfo[]
  capabilities: CapabilityInfo[]
  retired_slots: RetiredSlotInfo[]
}

export async function fetchMarketDataCapabilities(): Promise<CapabilityMatrix> {
  const r = await fetch('/api/v1/plugins/market-data/api/market/capabilities')
  if (!r.ok) throw new Error(`market capabilities: HTTP ${r.status}`)
  return (await r.json()) as CapabilityMatrix
}
