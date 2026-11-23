import { reachTag } from '@/lib/shell/stateWords'
import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'

/** Redis health hashes store connected as "True"/"False" (Python bool str), not "ok"/"connected". */
function ingestorConnected(status: IbGatewayStatusResponse | undefined): boolean {
  if (status?.ingestor_health == null) return false
  const raw = status.ingestor_health.connected
  if (raw == null) return false
  if (typeof raw === 'boolean') return raw
  const s = String(raw).trim().toLowerCase()
  return s === 'true' || s === '1' || s === 'yes' || s === 'ok' || s === 'connected'
}

/** Compact one-line verdict — avoids repeating the raw plugin summary in panels. */
export function compactIbGatewaySummary(status: IbGatewayStatusResponse | undefined): string {
  if (status == null) return 'Probing…'
  const dep = status.deployment?.ready ?? '—'
  const slots = status.slots ?? []
  const connected = slots.filter(s => s.connected).length
  const parts = [
    status.mode ?? '—',
    `dep ${dep}`,
    `slots ${connected}/${slots.length || '—'}`,
  ]
  if (status.redis_reachability != null) {
    parts.push(`redis ${status.redis_reachability}`)
  }
  parts.push(ingestorConnected(status) ? 'ingestor ok' : 'ingestor down')
  return parts.join(' · ')
}

export function ibGatewayExtraTags(status: IbGatewayStatusResponse | undefined): Array<{
  label: string
  variant: 'success' | 'warning' | 'danger' | 'neutral'
}> {
  const tags: Array<{ label: string; variant: 'success' | 'warning' | 'danger' | 'neutral' }> = []
  if (status?.redis_reachability != null) {
    // Name + the engine's seven words (箭10 2026-11-23): degraded reads warn, down reads fail.
    const r = status.redis_reachability
    const t = reachTag(r === 'ok' || r === 'degraded' || r === 'fail' ? r : 'unknown')
    tags.push({ label: `redis-ib ${t.tagLabel}`, variant: t.tagVariant })
  }
  const ing = reachTag(ingestorConnected(status) ? 'ok' : 'fail')
  tags.push({ label: `ingestor ${ing.tagLabel}`, variant: ing.tagVariant })
  return tags
}
