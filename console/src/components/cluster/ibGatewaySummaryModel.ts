import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'

function ingestorConnected(status: IbGatewayStatusResponse | undefined): boolean {
  if (status?.ingestor_health == null) return false
  const vals = Object.values(status.ingestor_health)
  return vals.some(v => String(v).toLowerCase() === 'ok' || String(v).toLowerCase() === 'connected')
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
    const r = status.redis_reachability
    tags.push({
      label: `redis-ib ${r}`,
      variant: r === 'ok' ? 'success' : r === 'degraded' ? 'warning' : r === 'fail' ? 'danger' : 'neutral',
    })
  }
  const ingOk = ingestorConnected(status)
  tags.push({
    label: ingOk ? 'ingestor ok' : 'ingestor down',
    variant: ingOk ? 'success' : 'danger',
  })
  return tags
}
