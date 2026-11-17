import { serverFetch } from '@/api/serverContact'
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

/** One rule of config/release-rules.yaml: which finished runs are a release of which line and environment. */
export type ReleaseRule = {
  match: { pipeline?: string; name_prefix?: string }
  lane: string
  env: string
  deploys: boolean
}

export type ReleaseListStatus = {
  rules_path?: string
  rules?: number
  rules_error?: string
  namespace?: string
  /** The rules as loaded, in order (absent from a platform older than 2026-11-17). */
  rule_list?: ReleaseRule[]
}

export type ReleaseListResponse = {
  status: ReleaseListStatus
  records: ReleaseRecord[]
  error?: string
}

export type RunningImageCell = {
  lane: string
  env: string
  absent?: boolean
  text: string
  title?: string
  error?: string
}

export type RunningImagesResponse = {
  cells: RunningImageCell[]
  error?: string
}

export async function fetchRunningImages(): Promise<RunningImagesResponse> {
  const r = await serverFetch('/api/v1/releases/running-images')
  if (!r.ok) throw new Error(`running images: HTTP ${r.status}`)
  const body = (await r.json()) as Partial<RunningImagesResponse>
  return {
    cells: Array.isArray(body.cells) ? body.cells : [],
    error: body.error,
  }
}

export async function fetchReleaseRecords(limit = 100): Promise<ReleaseListResponse> {
  const r = await serverFetch(`/api/v1/releases?limit=${limit}`)
  if (!r.ok) throw new Error(`releases: HTTP ${r.status}`)
  const body = (await r.json()) as Partial<ReleaseListResponse>
  return {
    status: body.status ?? {},
    records: Array.isArray(body.records) ? body.records : [],
    error: body.error,
  }
}
