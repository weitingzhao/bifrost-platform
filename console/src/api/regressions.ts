import { authedFetch } from '@/api/client'

/** A known regression on main, as GET /api/v1/bus/regressions answers it. */
export type Regression = { repo: string; introduced: string; impact: string; by: string; known_at: string }

/** The regressions recorded on repo's main and not yet fixed. */
export const fetchOpenRegressions = async (repo: string, signal?: AbortSignal): Promise<Regression[]> => {
  const query = new URLSearchParams({ repo, open: '1' })
  const value = await (await authedFetch('Known regressions', `/api/v1/bus/regressions?${query}`, { signal })).json() as { regressions?: unknown }
  if (!Array.isArray(value.regressions)) throw new Error('Known regressions: the answer has no list')
  return value.regressions as Regression[]
}
