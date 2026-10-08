/** GET /api/v1/releases — finished delivery runs and the commits they built. */

export type ReleaseRepoBuild = {
  sha: string
  source: string
}

export type ReleaseRecord = {
  run: string
  pipeline: string
  lane: string
  env: string
  deploys: boolean
  revision?: string
  tag?: string
  from_run?: string
  started_at: string
  completed_at: string
  repos: Record<string, ReleaseRepoBuild>
  missing?: string[]
  recorded_at: string
}

export type ReleaseListStatus = {
  rules_path?: string
  rules?: number
  rules_error?: string
  namespace?: string
}

export type ReleaseListResponse = {
  status: ReleaseListStatus
  records: ReleaseRecord[]
  error?: string
}

export async function fetchReleaseRecords(limit = 100): Promise<ReleaseListResponse> {
  const r = await fetch(`/api/v1/releases?limit=${limit}`)
  if (!r.ok) throw new Error(`releases: HTTP ${r.status}`)
  const body = (await r.json()) as Partial<ReleaseListResponse>
  return {
    status: body.status ?? {},
    records: Array.isArray(body.records) ? body.records : [],
    error: body.error,
  }
}
