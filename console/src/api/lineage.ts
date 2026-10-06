/** GET /api/v1/lineage — agent threads and the commits they stamped (commit trailers on the Gitea mirror). */

export type LineageLandedBy = 'sha' | 'change_id' | 'subject'

/** First recorded release in one lane/env that contained a commit. */
export type LineageReach = {
  lane: string
  env: string
  run: string
  at: string
  /** true: the run rolled out to a running environment; false: an image build deployed later. */
  deploys: boolean
}

export type LineageReleaseHead = {
  lane: string
  env: string
  run: string
  at: string
  deploys: boolean
  repos: Record<string, string>
}

export type LineageCommit = {
  repo: string
  sha: string
  subject: string
  at: string
  session?: string
  transcript?: string
  change_id?: string
  /** "main" (default branch) or the branch the commit was found on. */
  ref: string
  landed: boolean
  landed_sha?: string
  landed_by?: LineageLandedBy
  reached?: LineageReach[]
}

export type LineageThread = {
  /** Empty: commits with a Change-Id but no session trailer (Cursor, manual). */
  session: string
  transcripts: string[]
  link?: string
  first_at: string
  last_at: string
  commit_count: number
  landed: number
  /** commits per "lane/env" they reached */
  reached: Record<string, number>
  repos: { repo: string; commits: LineageCommit[] }[]
}

export type LineageCoverage = {
  repo: string
  main_commits: number
  with_session: number
  with_change_id: number
  agent_no_lineage: number
  branches: number
}

export type LineageResponse = {
  generated_at: string
  since: string
  days: number
  reachability: 'ok' | 'degraded' | 'fail' | 'unknown'
  threads: LineageThread[]
  coverage: LineageCoverage[]
  /** latest recorded release per lane/env */
  releases: LineageReleaseHead[]
  releases_error?: string
  errors: string[]
}

export async function fetchLineage(days: number, refresh = false): Promise<LineageResponse> {
  const q = new URLSearchParams({ days: String(days) })
  if (refresh) q.set('refresh', 'true')
  const r = await fetch(`/api/v1/lineage?${q.toString()}`)
  if (!r.ok) throw new Error(`lineage: HTTP ${r.status}`)
  return r.json() as Promise<LineageResponse>
}
