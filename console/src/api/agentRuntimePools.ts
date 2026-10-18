import { authedFetch } from '@/api/client'
import { record, text } from '@/api/agentRuntime'
import { signInSafeError } from '@/lib/signInError'

export type PoolView = {
  vendor_pool_id: string; tool: string; account: string; window: 'short' | 'weekly' | 'monthly'
  used_fraction: number | null; resets_at: string | null; measured_at: string | null
  measure_source: 'reading' | 'exit_text' | 'owner' | ''; cooldown_until: string | null
  cooling: boolean; pace: number | null; used: Record<string, number>; used_since: string
}
const invalid = (): never => { throw new Error('Quota: invalid platform response.') }
const finite = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v)
const timestamp = (v: unknown): v is string => typeof v === 'string' && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(v) && Number.isFinite(Date.parse(v))

/** Reject malformed rows rather than turning an unread quota into zero usage. */
export function parsePools(value: unknown): PoolView[] {
  const pools = record(value).pools
  if (!Array.isArray(pools)) return invalid()
  const seen = new Set<string>()
  return pools.map(value => {
    const p = record(value)
    const id = text(p.vendor_pool_id)
    const match = /^([a-z][a-z0-9-]{0,31}):([a-z0-9][a-z0-9-]{0,31}):(short|weekly|monthly)$/.exec(id)
    if (!match || match[1] !== p.tool || match[2] !== p.account || match[3] !== p.window || seen.has(id)) return invalid()
    seen.add(id)
    if (p.used_fraction !== null && (!finite(p.used_fraction) || p.used_fraction < 0 || p.used_fraction > 1)) return invalid()
    if (p.pace !== null && (!finite(p.pace) || p.pace < -1 || p.pace > 1)) return invalid()
    for (const key of ['resets_at', 'measured_at', 'cooldown_until']) if (p[key] !== null && !timestamp(p[key])) return invalid()
    if (!timestamp(p.used_since) || typeof p.cooling !== 'boolean' || (p.cooling && p.cooldown_until === null)) return invalid()
    if (!['reading', 'exit_text', 'owner', ''].includes(text(p.measure_source)) || typeof p.measure_source !== 'string') return invalid()
    if (!p.used || typeof p.used !== 'object' || Array.isArray(p.used) || Object.values(p.used).some(v => !finite(v) || v < 0)) return invalid()
    return p as PoolView
  })
}
async function poolError(prefix: string, response: Response, token = '') {
  const body = record(await response.json().catch(() => ({})))
  return new Error(signInSafeError(text(body.message) || text(body.error) || `${prefix}: HTTP ${response.status}`, token, response.status))
}
export async function fetchPools(signal?: AbortSignal): Promise<PoolView[]> {
  const response = await authedFetch('Quota', '/api/v1/agentrt/pools', { signal }, poolError)
  return parsePools(await response.json())
}
export async function recordPoolReading(id: string, usedFraction: number, resetsAt?: string) {
  const response = await authedFetch('Quota', `/api/v1/agentrt/pools/${encodeURIComponent(id)}/reading`, {
    method: 'POST', body: JSON.stringify({ idempotency_key: crypto.randomUUID(), used_fraction: usedFraction, ...(resetsAt ? { resets_at: resetsAt } : {}) }),
  }, poolError)
  const result = record(await response.json())
  if (result.vendor_pool_id !== id || result.recorded !== true) invalid()
}
