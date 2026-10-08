import type { DeliveryPipelineRunView, GitOpsApplicationView } from '@/api/deliveryTypes'
import type { ReleaseRecord } from '@/api/releases'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import {
  formatPipelineRunStatus,
  isPipelineRunFailed,
  isPipelineRunRunning,
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
}): VersionCell {
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

export function attentionRuns(
  groups: Array<{ pipeline: string; runs: DeliveryPipelineRunView[] }>,
): AttentionRun[] {
  const rows: AttentionRun[] = []
  for (const group of groups) {
    for (const run of group.runs) {
      let kind: AttentionRun['kind'] | null = null
      if (isPipelineRunRunning(run)) kind = 'running'
      else if (isPipelineRunFailed(run)) kind = 'failed'
      if (kind == null) continue
      rows.push({
        pipeline: group.pipeline,
        name: run.name,
        status: formatPipelineRunStatus(run),
        kind,
        started: formatInstant(run.start_time),
        revision: run.revision?.trim() || '—',
      })
    }
  }
  rows.sort((a, b) => {
    if (a.kind !== b.kind) return a.kind === 'running' ? -1 : 1
    if (a.started === b.started) return a.name.localeCompare(b.name)
    return a.started < b.started ? 1 : -1
  })
  return rows
}
