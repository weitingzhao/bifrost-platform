import { authedFetch } from './client'

/*
 * The CI scorecard (Work > CI): how each repository uses CI, and CI as their provider, scored by the platform from the
 * runs the cluster retains (GET /api/v1/ci/scorecard). Every item is a verdict with the raw number behind it; the pass
 * lines are CI's and come with the answer.
 */

export type CiVerdict = 'pass' | 'fail' | 'unmeasured'
export type CiScoreItem = { id: string; label: string; verdict: CiVerdict; value: string; samples: number }
export type CiScoreStandard = { id: string; label: string; pass_when: string }
export type CiScoreRepo = { repo: string; pipeline: string; tier: string; overall: CiVerdict; items: CiScoreItem[] }
export type CiScorecard = {
  generated_at: string
  window_days: number
  standards: CiScoreStandard[]
  repos: CiScoreRepo[]
  provider: { overall: CiVerdict; items: CiScoreItem[] }
}

const record = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const items = (v: unknown): boolean => Array.isArray(v) && v.every(i => record(i) && typeof i.id === 'string' && typeof i.verdict === 'string' && typeof i.value === 'string')

export async function fetchCiScorecard(signal?: AbortSignal): Promise<CiScorecard> {
  const value: unknown = await (await authedFetch('CI scorecard', '/api/v1/ci/scorecard', { signal })).json()
  if (!record(value) || !Array.isArray(value.standards) || !Array.isArray(value.repos) || !record(value.provider) || !items(value.provider.items)
    || !value.repos.every(r => record(r) && typeof r.repo === 'string' && items(r.items))) {
    throw new Error('CI scorecard: incomplete answer; the scores are not known')
  }
  return value as unknown as CiScorecard
}
