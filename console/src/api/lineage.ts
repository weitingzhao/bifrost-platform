import { authedFetch } from './client'

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
  /** Human name: set by hand ("manual") or the session title synced from its transcripts ("transcript"). */
  title?: string
  title_source?: 'manual' | 'transcript'
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
  /** when Gitea last fetched this repo from GitHub (mirrors only) */
  mirror_updated?: string
}

/** the mirror fetch a build asked for before scanning (absent when one ran under 2 min ago) */
export type LineageMirrorSync = {
  requested_at: string
  settled: boolean
  /** still fetching when the scan started: read at their previous state */
  pending?: string[]
  errors?: string[]
}

export type LineageGraphCommit = {
  sha: string
  subject: string
  at: string
  session?: string
  change_id?: string
  /** Claude co-author line but no session trailer (agent work before the hooks). */
  agent?: boolean
  /** branch commits: how the change reached the default branch ("" / absent = not yet) */
  landed_sha?: string
  landed_by?: LineageLandedBy
}

export type LineageGraphMarker = {
  lane: string
  env: string
  run: string
  at: string
  deploys: boolean
  /** default-branch commit the release built */
  sha: string
}

export type LineageRepoGraph = {
  repo: string
  default: string
  /** default branch in the window, newest first */
  main: LineageGraphCommit[]
  /** branches with at least one change not on the default branch */
  /** fork_sha: parent of the branch's oldest commit in the window (where it left the default branch) */
  branches: { name: string; commits: LineageGraphCommit[]; fork_sha?: string }[]
  /** releases that built a default-branch commit in the window, newest first */
  markers: LineageGraphMarker[]
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
  /** only with graph=true */
  graph?: LineageRepoGraph[]
  mirror_sync?: LineageMirrorSync
  errors: string[]
}

/** PUT /api/v1/lineage/thread-title (operator). An empty title clears the hand-set name. */
export async function setThreadTitle(session: string, title: string): Promise<void> {
  await authedFetch('thread title', '/api/v1/lineage/thread-title', {
    method: 'PUT',
    body: JSON.stringify({ session, title }),
  })
}

export async function fetchLineage(days: number, refresh = false, graph = false): Promise<LineageResponse> {
  const q = new URLSearchParams({ days: String(days) })
  if (refresh) q.set('refresh', 'true')
  if (graph) q.set('graph', 'true')
  const r = await fetch(`/api/v1/lineage?${q.toString()}`)
  if (!r.ok) throw new Error(`lineage: HTTP ${r.status}`)
  return r.json() as Promise<LineageResponse>
}

/** One agent thread with commits on a branch. */
export type BranchThread = { session: string; transcripts?: string[]; title?: string }

/** GET /api/v1/lineage/branches — a non-default branch measured against its default branch. */
export type BranchHealth = {
  repo: string
  branch: string
  head_sha: string
  head_at: string
  /** commits the branch has that the default branch does not */
  ahead: number
  /** of those, changes not on the default branch in any form (Change-Id / subject) */
  open: number
  oldest_open_at?: string
  /** default-branch commits since the branch forked; a floor when behind_is_floor */
  behind: number
  behind_is_floor?: boolean
  fork_sha?: string
  /** open: unlanded work · landed: every change is on main in another form (leftover) · even: merged */
  status: 'open' | 'landed' | 'even'
  threads: BranchThread[]
  commits: LineageGraphCommit[]
}

export type BranchesResponse = {
  generated_at: string
  reachability: 'ok' | 'degraded' | 'fail' | 'unknown'
  branches: BranchHealth[]
  errors: string[]
}

export async function fetchBranches(refresh = false): Promise<BranchesResponse> {
  const r = await fetch(`/api/v1/lineage/branches${refresh ? '?refresh=true' : ''}`)
  if (!r.ok) throw new Error(`lineage branches: HTTP ${r.status}`)
  return r.json() as Promise<BranchesResponse>
}
