import { authedFetch } from '@/api/client'

/**
 * GET /api/v1/bus/effect — what the bus's work does, each reading computed
 * from its source when asked (2026-11-17: the Owner could not see what the
 * communication work does). A reading the platform could not take carries
 * `unmeasured` instead of a value.
 */
export type BusEffectItem = { what: string; when: string; ref?: string }
export type BusEffectRow = {
  key: string
  label: string
  value: string
  /** What the value is compared with: a baseline the system took at a time, or the previous window; absent when there is none. */
  before?: string
  before_kind?: 'baseline' | 'previous'
  how: string
  unmeasured?: string
  items?: BusEffectItem[]
}
export type BusEffect = { generated_at: string; env: string; rows: BusEffectRow[] }

export async function fetchBusEffect(): Promise<BusEffect> {
  const r = await authedFetch('bus effect', '/api/v1/bus/effect')
  const body = (await r.json()) as BusEffect
  return { ...body, rows: body.rows ?? [] }
}
