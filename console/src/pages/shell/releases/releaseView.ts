import { formatTimeRemaining, type ApprovalItem } from '@/api/approvals'
import type { DeliveryPipelineRunView, GitOpsApplicationView, ReleaseWindowResponse } from '@/api/deliveryTypes'
import type { ReleaseRecord, RunningImageCell } from '@/api/releases'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import {
  formatPipelineRunStatus,
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
} from '@/lib/delivery/pipelineRunAskPack'

/** Catalog id for Argo rollback. PROD application names classify as C on create. */
export const RELEASE_ROLLBACK_ACTION = 'gitops_rollback_app'

export const REQUEST_ROLLBACK_LABEL = 'Request rollback'

export const RELEASE_RECORD_LIMIT = 40

export const VERSION_ROWS = [
  { lane: 'platform', label: 'Platform' },
  { lane: 'trade', label: 'Trade' },
  { lane: 'research', label: 'Research' },
  { lane: 'market-data', label: 'Plugins' },
  { lane: 'agent', label: 'Mac mini agent' },
] as const

export type VersionLane = (typeof VERSION_ROWS)[number]['lane']

/** Lanes whose STG/PROD cells come from GET /api/v1/releases/running-images. */
export const LIVE_IMAGE_LANES = new Set<VersionLane>(['research', 'market-data', 'agent'])

export type VersionCell = {
  text: string
  title: string
}

export type AttentionRun = {
  pipeline: string
  name: string
  status: string
  kind: 'running' | 'failed'
  started: string
  revision: string
}

type VersionApp = {
  name: string
  destination_namespace?: string
  revision?: string
}

/**
 * Name rule from the actions catalog (ProdApp): an application is PROD when
 * its name is "prod", starts with "prod-", ends with "-prod", or contains
 * "-prod-". Those classify as tier C. Every other name is tier B and would
 * run immediately, so this page does not offer a button for them.
 */
export function catalogProdApp(name: string): boolean {
  const n = name.trim().toLowerCase()
  return n === 'prod' || n.startsWith('prod-') || n.endsWith('-prod') || n.includes('-prod-')
}

export function rollbackReason(appName: string): string {
  return `Roll back Argo CD application ${appName} to the previous revision`
}

export function rollbackUndo(): string {
  return 'Sync the application forward to its target revision'
}

/** Deliver and image-build pipelines. CI and smoke runs are not release runs. */
export function isReleasePipeline(name: string): boolean {
  return name.startsWith('bifrost-deliver-') || name.startsWith('bifrost-build-')
}

export function namespaceForLaneEnv(lane: string, env: 'stg' | 'prod'): string | undefined {
  if (lane === 'platform' && env === 'stg') return deliveryTargetById('platform-stg').namespace
  if (lane === 'platform' && env === 'prod') return deliveryTargetById('platform-prod').namespace
  if (lane === 'trade' && env === 'stg') return deliveryTargetById('trade-stg').namespace
  if (lane === 'trade' && env === 'prod') return deliveryTargetById('trade-prod').namespace
  return undefined
}

export function formatInstant(iso: string | undefined): string {
  if (iso == null || iso === '' || iso.startsWith('0001-')) return '—'
  const t = Date.parse(iso)
  if (!Number.isFinite(t)) return '—'
  return `${new Date(t).toISOString().replace('T', ' ').slice(0, 16)}Z`
}

export function shortSha(value: string | undefined): string {
  const v = value?.trim() ?? ''
  if (v === '') return ''
  if (v.length <= 12) return v
  return v.slice(0, 7)
}

function shortRepo(repo: string): string {
  return repo.replace(/^bifrost-/, '')
}

export function formatBuiltFrom(record: ReleaseRecord): string {
  const parts = Object.entries(record.repos ?? {})
    .filter(([, build]) => (build?.sha ?? '') !== '')
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([repo, build]) => `${shortRepo(repo)} ${shortSha(build.sha)}`)
  return parts.length > 0 ? parts.join(' · ') : '—'
}

export function formatBuiltFromTitle(record: ReleaseRecord): string {
  return Object.entries(record.repos ?? {})
    .filter(([, build]) => (build?.sha ?? '') !== '')
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([repo, build]) => `${repo} ${build.sha}`)
    .join('\n')
}

function formatReleaseVersion(record: ReleaseRecord): string {
  const tag = record.tag?.trim() ?? ''
  const built = formatBuiltFrom(record)
  const hasBuilt = built !== '—'
  if (tag !== '' && hasBuilt) return `${tag} · ${built}`
  if (tag !== '') return tag
  if (hasBuilt) return built
  const rev = shortSha(record.revision)
  return rev
}

function newestDeploy(records: ReleaseRecord[], lane: string, env: string): ReleaseRecord | undefined {
  const wantLane = lane.toLowerCase()
  const wantEnv = env.toLowerCase()
  return records
    .filter(r => r.deploys && r.lane.toLowerCase() === wantLane && r.env.toLowerCase() === wantEnv)
    .sort((a, b) => Date.parse(b.completed_at) - Date.parse(a.completed_at))[0]
}

function gitopsText(apps: VersionApp[], namespace: string): { text: string; title: string } | undefined {
  const matches = apps.filter(
    app => app.destination_namespace === namespace && (app.revision?.trim() ?? '') !== '',
  )
  if (matches.length === 0) return undefined
  const unique = [...new Set(matches.map(app => app.revision?.trim() ?? ''))]
  const text =
    unique.length === 1
      ? shortSha(unique[0])
      : matches.map(app => `${app.name} ${shortSha(app.revision)}`).join(' · ')
  const title = matches.map(app => `${app.name} ${app.revision}`).join('\n')
  return { text, title: `Argo CD revision\n${title}` }
}

export function versionCell(input: {
  records: ReleaseRecord[]
  lane: string
  env: 'stg' | 'prod'
  apps: VersionApp[]
  runningImages?: RunningImageCell[]
}): VersionCell {
  if (LIVE_IMAGE_LANES.has(input.lane as VersionLane)) {
    const cell = input.runningImages?.find(item => item.lane === input.lane && item.env === input.env)
    if (cell != null && cell.text !== '') {
      return { text: cell.text, title: cell.title ?? '' }
    }
    return { text: '—', title: '' }
  }
  const record = newestDeploy(input.records, input.lane, input.env)
  if (record != null) {
    const text = formatReleaseVersion(record)
    if (text !== '') {
      const built = formatBuiltFromTitle(record)
      const title = [record.run, record.tag?.trim() ?? '', built].filter(part => part !== '').join('\n')
      return { text, title }
    }
  }
  const namespace = namespaceForLaneEnv(input.lane, input.env)
  if (namespace != null) {
    const fromGit = gitopsText(input.apps, namespace)
    if (fromGit != null && fromGit.text !== '') return fromGit
  }
  return { text: '—', title: '' }
}

export function prodRollbackApps(apps: GitOpsApplicationView[]): GitOpsApplicationView[] {
  return apps
    .filter(app => catalogProdApp(app.name))
    .sort((a, b) => a.name.localeCompare(b.name))
}

export type SupersededRun = AttentionRun & {
  /** The later successful run of the same pipeline. */
  supersededBy: string
}

export type ReleaseRunSplit = {
  /** Running runs, then failed runs no later success has covered. */
  inProgress: AttentionRun[]
  /** Failed runs a later success of the same pipeline covered. History only. */
  superseded: SupersededRun[]
}

function instantMs(iso: string | undefined): number {
  if (iso == null || iso === '' || iso.startsWith('0001-')) return Number.NaN
  return Date.parse(iso)
}

function newestFirst(a: AttentionRun, b: AttentionRun): number {
  if (a.started === b.started) return a.name.localeCompare(b.name)
  return a.started < b.started ? 1 : -1
}

/**
 * A failed run moves to history once a later run of the same pipeline
 * succeeded. Each release pipeline ships to one environment
 * (release-rules.yaml), so the pipeline name is the pipeline-and-environment
 * key. Successes come from the run list and from release records, which
 * outlive the CI system's run retention.
 */
export function splitReleaseRuns(
  groups: Array<{ pipeline: string; runs: DeliveryPipelineRunView[] }>,
  records: ReleaseRecord[] = [],
): ReleaseRunSplit {
  const inProgress: AttentionRun[] = []
  const superseded: SupersededRun[] = []
  for (const group of groups) {
    const successes = [
      ...group.runs
        .filter(isPipelineRunSucceeded)
        .map(run => ({ name: run.name, at: instantMs(run.start_time) })),
      ...records
        .filter(record => record.pipeline === group.pipeline)
        .map(record => ({ name: record.run, at: instantMs(record.started_at) })),
    ]
      .filter(success => Number.isFinite(success.at))
      .sort((a, b) => a.at - b.at)

    for (const run of group.runs) {
      let kind: AttentionRun['kind'] | null = null
      if (isPipelineRunRunning(run)) kind = 'running'
      else if (isPipelineRunFailed(run)) kind = 'failed'
      if (kind == null) continue
      const row: AttentionRun = {
        pipeline: group.pipeline,
        name: run.name,
        status: formatPipelineRunStatus(run),
        kind,
        started: formatInstant(run.start_time),
        revision: run.revision?.trim() || '—',
      }
      const startedAt = instantMs(run.start_time)
      const later =
        kind === 'failed' && Number.isFinite(startedAt)
          ? successes.find(success => success.at > startedAt && success.name !== run.name)
          : undefined
      if (later != null) superseded.push({ ...row, supersededBy: later.name })
      else inProgress.push(row)
    }
  }
  inProgress.sort((a, b) => {
    if (a.kind !== b.kind) return a.kind === 'running' ? -1 : 1
    return newestFirst(a, b)
  })
  superseded.sort(newestFirst)
  return { inProgress, superseded }
}

/** Compact elapsed time: `45m`, `3h`, `2d 4h`. */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '—'
  const totalMin = Math.floor(ms / 60_000)
  if (totalMin < 60) return `${totalMin}m`
  const hours = Math.floor(totalMin / 60)
  if (hours < 48) return `${hours}h`
  const days = Math.floor(hours / 24)
  const rest = hours % 24
  return rest === 0 ? `${days}d` : `${days}d ${rest}h`
}

export type ReleaseWindowView = {
  state: 'no-token' | 'loading' | 'error' | 'free' | 'held'
  text: string
  detail: string
}

export const RELEASE_WINDOW_FREE = 'Release window free'

export const NO_VIEWER_TOKEN = 'This device has no viewer token'

export function releaseWindowView(input: {
  hasToken: boolean
  isLoading: boolean
  error: string | null
  data: ReleaseWindowResponse | undefined
  now?: number
}): ReleaseWindowView {
  if (!input.hasToken) return { state: 'no-token', text: 'Unknown', detail: NO_VIEWER_TOKEN }
  if (input.error != null) return { state: 'error', text: 'Unknown', detail: input.error }
  if (input.isLoading || input.data == null) return { state: 'loading', text: 'Loading…', detail: '' }
  const held = input.data.window
  if (!input.data.open || held == null) return { state: 'free', text: RELEASE_WINDOW_FREE, detail: '' }
  const now = input.now ?? Date.now()
  const parts = [
    held.what.trim() !== '' ? held.what : '',
    held.env.trim() !== '' ? held.env : '',
    held.expires_at != null && held.expires_at !== '' ? formatTimeRemaining(held.expires_at, now) : '',
    `held ${formatDuration(now - instantMs(held.started_at))}`,
    held.reason?.trim() ?? '',
  ].filter(part => part !== '')
  return { state: 'held', text: `Held by ${held.who}`, detail: parts.join(' · ') }
}

/** Approval actions that ship or roll back a release. */
export const RELEASE_APPROVAL_ACTIONS: ReadonlySet<string> = new Set([
  'start_pipeline_run',
  'gitops_sync_app',
  'gitops_rollback_app',
])

/** Pending release requests, longest waiting first. */
export function pendingReleaseApprovals(items: ApprovalItem[]): ApprovalItem[] {
  return items
    .filter(item => item.status === 'pending' && RELEASE_APPROVAL_ACTIONS.has(item.action))
    .sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at))
}

function paramText(params: Record<string, unknown>, key: string): string {
  const value = params[key]
  return typeof value === 'string' ? value.trim() : ''
}

/** What the request ships: pipeline or application, revision, tag. */
export function approvalSubject(item: ApprovalItem): string {
  const params = item.params ?? {}
  const revision = paramText(params, 'revision')
  const parts = [
    paramText(params, 'name'),
    revision === '' ? '' : shortSha(revision) || revision,
    paramText(params, 'tag'),
  ].filter(part => part !== '')
  return parts.length > 0 ? parts.join(' · ') : '—'
}

export function approvalHref(id: string): string {
  return `#approvals?id=${encodeURIComponent(id)}`
}
