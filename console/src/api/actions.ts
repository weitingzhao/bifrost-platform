import { authedFetch } from '@/api/client'

/** One row of GET /api/v1/actions. Tier is the resting level; classify happens on create. */
export type CatalogAction = {
  id: string
  tier: string
  description: string
  params: Array<{ name: string; type: string; required: boolean; in?: string }>
}

function isCatalogAction(value: unknown): value is CatalogAction {
  if (value == null || typeof value !== 'object') return false
  const row = value as CatalogAction
  return typeof row.id === 'string' && typeof row.tier === 'string'
}

export async function fetchActionCatalog(): Promise<CatalogAction[]> {
  const r = await authedFetch('actions', '/api/v1/actions')
  const body: unknown = await r.json()
  if (!Array.isArray(body)) throw new Error('actions: unexpected list')
  return body.filter(isCatalogAction)
}
