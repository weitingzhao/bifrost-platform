import { authedFetch } from '@/api/client'

/** One judgement of the PROD head-release loop, as GET /api/v1/delivery/prod-judgements answers it (headrelease.ProdJudgement). */
export type ProdJudgement = {
  at: string
  result: string
  record: { verdict?: string; from?: string; to?: string; commits?: number | string; failing?: string[]; reasons?: Record<string, string>; run?: string; text?: string }
}
export type ProdJudgements = { mode: string; judgements: ProdJudgement[] }

const record = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)

/** The newest judgements of the last 7 days, newest first. */
export const fetchProdJudgements = async (signal?: AbortSignal): Promise<ProdJudgements> => {
  const value: unknown = await (await authedFetch('PROD judgements', '/api/v1/delivery/prod-judgements?limit=5', { signal })).json()
  if (!record(value) || typeof value.mode !== 'string' || !Array.isArray(value.judgements)
    || !value.judgements.every(j => record(j) && typeof j.at === 'string' && typeof j.result === 'string' && record(j.record))) {
    throw new Error('PROD judgements: incomplete answer; why PROD was held is not known')
  }
  return value as unknown as ProdJudgements
}
