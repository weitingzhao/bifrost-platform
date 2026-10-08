import type { StgSmokeResponse } from './deliveryTypes'

export async function fetchStgSmoke(): Promise<StgSmokeResponse> {
  const r = await fetch('/api/v1/delivery/stg/smoke')
  if (!r.ok) throw new Error(`stg smoke: HTTP ${r.status}`)
  return r.json() as Promise<StgSmokeResponse>
}
