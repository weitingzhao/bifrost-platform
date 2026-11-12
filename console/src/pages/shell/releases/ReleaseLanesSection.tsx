import { DenseTag } from '@bifrost/ui'
import type { ApprovalItem } from '@/api/approvals'
import type { DeliveryPipelineRunView } from '@/api/deliveryTypes'
import type { ReleaseRecord, RunningImageCell } from '@/api/releases'
import { OpsSection } from '@/components/layout/OpsSection'
import { formatPipelineRunStatus, isPipelineRunFailed, isPipelineRunRunning, isPipelineRunSucceeded } from '@/lib/delivery/pipelineRunAskPack'
import { NO_VIEWER_TOKEN, RELEASE_RECORD_LIMIT, approvalHref, formatBuiltFrom, formatBuiltFromTitle, formatDuration, formatInstant, shortSha,
  type ReleaseWindowView } from '@/pages/shell/releases/releaseView'

/** GET /api/v1/agentrt/rules/landing:dev (agentrtapi getRule): `in_force` is rules.Rule.Doc of the landing rule. */
export type LandingRuleRead = { in_force?: { rule_version?: number; repos?: { repo: string; limits?: { max_in_flight?: number } }[]
  limits?: { max_in_flight_per_repo?: number } } | null }

/** One read: `data` once answered, `error` once failed, neither while it is read. */
export type LaneRead<T> = { data?: T; error: string | null }

export type ReleaseLanesProps = {
  now: number
  hasToken: boolean
  window: ReleaseWindowView
  records: LaneRead<ReleaseRecord[]>
  /** The release pipelines delivery/pipelines lists. */
  pipelines: LaneRead<string[]>
  /** By pipeline; a pipeline missing here was not read. */
  runs: Record<string, LaneRead<DeliveryPipelineRunView[]>>
  images: LaneRead<RunningImageCell[]>
  rule: LaneRead<LandingRuleRead>
  approvals: ApprovalItem[]
}

/** `rev`: the repository a run's `revision` parameter clones (other repositories take their own parameters). */
type Lane = { id: string; lane: string; repos: string[]; rev?: string; stg?: string; prod?: string; build?: string; image?: string; prodNote?: string }

/**
 * Release lines: config/release-rules.yaml (records), delivery/release_window.go (pipelines), config/running-images.yaml
 * (live images). Repositories are the clones of each line's pipelines (infra k8s/cicd/tekton/pipeline-deliver-*.yaml).
 */
const LANES: Lane[] = [
  { id: 'platform', lane: 'platform', repos: ['bifrost-platform', 'bifrost-ui', 'bifrost-trade-infra'], rev: 'platform', stg: 'bifrost-deliver-platform', prod: 'bifrost-deliver-platform-prod' },
  { id: 'trade', lane: 'trade', repos: [...['core', 'worker', 'api', 'frontend'].map(r => `bifrost-trade-${r}`), 'bifrost-ui', 'bifrost-trade-infra'],
    stg: 'bifrost-deliver-stg', prod: 'bifrost-deliver-prod', prodNote: 'pinned PROD runs (release.sh prod, bifrost-deliver-prod-pinned-*) are not in this run list: '
      + 'one in flight, one that rolled out and then failed its checks, or one that succeeded in the last 5 minutes before the recorder wrote it does not show here' },
  { id: 'research', lane: 'research', repos: ['bifrost-research'], rev: 'research', build: 'bifrost-deliver-research', image: 'research-api' },
  { id: 'market-data', lane: 'market-data', repos: ['bifrost-platform-plugin-market-data'], rev: 'market-data', build: 'bifrost-build-market-data', image: 'market-data-api' },
  { id: 'flex-query', lane: 'market-data', repos: ['bifrost-platform-plugin-flex-query'], rev: 'flex-query', build: 'bifrost-build-flex-query', image: 'flex-query-api' },
  { id: 'ib-gateway', lane: 'market-data', repos: ['bifrost-platform-plugin'], rev: 'plugin', build: 'bifrost-build-ib-gateway', image: 'ib-gateway' },
]

const IN_FLIGHT_GAP = 'no per-repository landing read exists. landing/operations/{op} needs an operation id; landing/missions/* folds the whole event stream (the read that ran PROD platform-api out of memory on 2026-10-31) and is not used here.'

type Note = { text: string; title?: string; href?: string }
/** `at`: when what the cell shows finished, NaN when unknown. */
type Cell = { text: string; title?: string; notes: Note[]; mono?: boolean; at?: number }

const READING: Cell = { text: 'Reading…', notes: [] }
const unknown = (why: string): Cell => ({ text: 'Unknown', notes: [{ text: why }] })
const short = (repo: string) => repo.replace(/^bifrost-/, '')

/** Milliseconds, or NaN for a missing, unparsable or Go zero time. */
const time = (iso: string | undefined) => (iso == null || iso.startsWith('0001-') ? Number.NaN : Date.parse(iso))
const rank = (iso: string | undefined) => (Number.isFinite(time(iso)) ? time(iso) : -1)

function ago(iso: string | undefined, now: number): string | null {
  return Number.isFinite(time(iso)) ? `${formatDuration(now - time(iso))} ago` : null
}

const when = (iso: string | undefined, now: number) => (ago(iso, now) == null ? 'time unknown' : `${formatInstant(iso)} · ${ago(iso, now)}`)

/** A revision names a commit only as a full SHA; a branch such as `main` moved since, so its commit is not recorded. */
const revisionText = (rev: string | undefined) => (rev == null || rev.trim() === '' ? 'revision not recorded'
  : /^[0-9a-f]{40}$/i.test(rev.trim()) ? shortSha(rev) : `${rev.trim()} (commit not recorded)`)

function recordCell(record: ReleaseRecord, now: number): Cell {
  const built = formatBuiltFrom(record)
  // `missing` holds clone tasks whose commit is unknown, and build tasks owing an image digest ("build-x (digest)").
  const digests = (record.missing ?? []).filter(m => m.endsWith(' (digest)')).map(m => m.replace(/ \(digest\)$/, ''))
  const commits = (record.missing ?? []).filter(m => !m.endsWith(' (digest)'))
  const lost = [...(commits.length > 0 ? [{ text: `commit not recorded: ${commits.join(', ')}` }] : []),
    ...(digests.length > 0 ? [{ text: `digest not recorded: ${digests.join(', ')}` }] : [])]
  return { mono: true, at: time(record.completed_at), text: built !== '—' ? built : revisionText(record.revision), title: `${record.run}\n${formatBuiltFromTitle(record)}`,
    notes: [{ text: when(record.completed_at, now) }, ...lost] }
}

/** A run's revision, named by the repository its `revision` parameter clones when the line has one. */
const runRevision = (run: DeliveryPipelineRunView, rev?: string) => (rev != null ? `${rev} ${revisionText(run.revision)}` : revisionText(run.revision))

const runCell = (run: DeliveryPipelineRunView, now: number, rev?: string): Cell => ({ mono: true, at: time(run.completion_time),
  text: runRevision(run, rev), title: run.name, notes: [{ text: when(run.completion_time, now) }] })

/** Why a pipeline's run list is unread: the reason, null once read, undefined while the list or the pipelines are read. */
function unreadWhy(p: ReleaseLanesProps, pipeline: string): string | null | undefined {
  const list = p.runs[pipeline]
  if (list != null) return list.error ?? (list.data == null ? undefined : null)
  return p.pipelines.error != null ? `pipelines: ${p.pipelines.error}` : p.pipelines.data == null ? undefined : 'pipeline not listed'
}

/**
 * What a line last shipped: the newest matching release record against the newest successful run, the later by
 * completion time. Only run identity makes them one release (the record names the run): then the record's exact commits
 * show. A shared revision proves nothing (`main` moves; each repository is cloned on its own), so different runs each
 * show their own. Times that cannot be compared show the one whose time is known and name the other; never the older alone.
 */
function shipped(p: ReleaseLanesProps, pipeline: string, keep: (r: ReleaseRecord) => boolean, rev?: string): Cell | null {
  const record = (p.records.data ?? []).filter(keep).sort((a, b) => rank(b.completed_at) - rank(a.completed_at))[0]
  const run = (p.runs[pipeline]?.data ?? []).filter(isPipelineRunSucceeded).sort((a, b) => rank(b.completion_time) - rank(a.completion_time))[0]
  // Records are written only for successful runs, every five minutes (releases.Service.Tick); an unread run list hides what came since.
  const unread = unreadWhy(p, pipeline)
  const stale = record != null && unread != null ? [{ text: `runs not read: ${unread}; a newer run may not be recorded yet (the recorder runs every 5 minutes)` }] : []
  const pick = (): Cell | null => {
    if (record == null || run == null) return record != null ? recordCell(record, p.now) : run != null ? runCell(run, p.now, rev) : null
    const [fromRecord, fromRun] = [recordCell(record, p.now), runCell(run, p.now, rev)]
    const [recordAt, runAt] = [time(record.completed_at), time(run.completion_time)]
    if (record.run === run.name) {
      return rank(run.completion_time) > rank(record.completed_at) ? { ...fromRecord, at: runAt, notes: [...fromRun.notes, ...fromRecord.notes.slice(1)] } : fromRecord
    }
    if (Number.isFinite(recordAt) && Number.isFinite(runAt)) return runAt > recordAt ? fromRun : fromRecord
    const [shown, other] = Number.isFinite(runAt) ? [fromRun, fromRecord] : [fromRecord, fromRun]
    return { ...shown, notes: [...shown.notes, { text: `also ${other.text} · ${other.notes[0]?.text ?? 'time unknown'}; which is newer is unknown`, title: other.title }] }
  }
  const cell = pick()
  return cell == null ? null : { ...cell, notes: [...cell.notes, ...stale] }
}

function missing(p: ReleaseLanesProps, pipeline: string): Cell {
  const unread = unreadWhy(p, pipeline)
  const why = [p.records.error != null ? `release records: ${p.records.error}` : '', unread != null ? `${pipeline} runs not read (${unread})` : ''].filter(Boolean)
  if (why.length > 0) return unknown(why.join('; '))
  if (p.records.data == null || unread === undefined) return READING
  return { text: `None in the newest ${RELEASE_RECORD_LIMIT} records or the run list`, notes: [] }
}

/** Runs of this pipeline in flight, and pending requests to start it (#approvals?id=). */
function inFlight(pipeline: string | undefined, verb: string, p: ReleaseLanesProps, rev?: string): Note[] {
  const running = (p.runs[pipeline ?? '']?.data ?? []).filter(isPipelineRunRunning)
    .map(run => ({ text: `${verb} ${runRevision(run, rev)} · started ${ago(run.start_time, p.now) ?? 'at an unrecorded time'}`, title: run.name }))
  const waiting = p.approvals.filter(a => a.action === 'start_pipeline_run' && a.params?.name === pipeline)
    .map(a => ({ text: 'Approval waiting ›', title: a.id, href: approvalHref(a.id) }))
  return [...running, ...waiting]
}

/** The last successful delivery. deliver-* roll out before they verify, so a later run that ended unsuccessfully may have rolled out. */
function deployCell(row: Lane, env: 'stg' | 'prod', p: ReleaseLanesProps): Cell {
  const pipeline = (env === 'stg' ? row.stg : row.prod) ?? ''
  const cell = shipped(p, pipeline, r => r.deploys && r.lane === row.lane && r.env === env, row.rev) ?? missing(p, pipeline)
  const shownAt = cell.at ?? Number.NaN
  const ended = (r: DeliveryPipelineRunView) => r.completion_time ?? r.start_time
  const failed = (p.runs[pipeline]?.data ?? []).filter(isPipelineRunFailed).filter(r => !Number.isFinite(shownAt) || rank(ended(r)) > shownAt)
    .sort((a, b) => rank(ended(b)) - rank(ended(a)))[0]
  const risk = failed == null ? [] : [{ title: failed.name,
    text: `${Number.isFinite(shownAt) ? 'newer ' : ''}run ${failed.name} ${formatPipelineRunStatus(failed)} at ${when(ended(failed), p.now)} — it may have rolled out; what runs now is not confirmed` }]
  const pinned = env === 'prod' && row.prodNote != null ? [{ text: row.prodNote }] : []
  return { ...cell, notes: [...cell.notes, ...risk, ...inFlight(pipeline, 'Delivering', p, row.rev), ...pinned] }
}

/** Research and plugins: the live image (running-images), and under PROD the last image build. */
function imageCell(row: Lane, env: 'stg' | 'prod', p: ReleaseLanesProps): Cell {
  const live = p.images.data?.find(c => c.lane === row.lane && c.env === env)
  const part = live?.text.split(' · ').find(t => t.startsWith(`${row.image} `))
  const cell = p.images.error != null ? unknown(`running-images: ${p.images.error}`) : p.images.data == null ? READING
    : live?.absent === true ? { text: live.text, notes: [] }
    : part != null ? { mono: true, text: part, title: live?.title, notes: [{ text: 'image in the Deployment spec; rollout state not read' }] }
    : unknown(live == null ? `running-images has no ${env} cell for ${row.lane}` : live.text)
  if (env === 'stg') return cell
  const build = shipped(p, row.build ?? '', r => r.pipeline === row.build, row.rev)
  const built = build == null ? [] : [{ text: `last image build ${build.text} · ${build.notes.map(n => n.text).join('; ')}`, title: build.title }]
  return { ...cell, notes: [...cell.notes, ...built, ...inFlight(row.build, 'Building', p, row.rev)] }
}

function landingCell(row: Lane, p: ReleaseLanesProps): Cell {
  if (!p.hasToken) return unknown(NO_VIEWER_TOKEN)
  if (p.rule.error != null) return unknown(p.rule.error)
  if (p.rule.data == null) return READING
  const rule = p.rule.data.in_force
  if (rule == null) return { text: 'No landing rule in force', notes: [] }
  // A repository's own limit replaces the default (rules.Landing.EffectiveLimits).
  const allowed = (rule.repos ?? []).filter(r => row.repos.includes(r.repo))
    .map(r => `${short(r.repo)} (at most ${r.limits?.max_in_flight ?? rule.limits?.max_in_flight_per_repo ?? '?'} in flight)`)
  if (allowed.length === 0) return { text: `Not allowed by landing:dev v${rule.rule_version ?? '?'}`, notes: [] }
  return { text: 'Unknown', notes: [{ text: `landing:dev v${rule.rule_version ?? '?'} allows ${allowed.join(', ')}; live state and landings in flight not read` }] }
}

function LaneCell({ label, cell }: { label: string; cell: Cell }) {
  return <div className="min-w-0 text-sm [overflow-wrap:anywhere]">
    <span className="mr-1.5 text-xs font-semibold text-muted-foreground">{label}</span>
    <span className={cell.mono ? 'font-mono-tabular' : ''} title={cell.title}>{cell.text}</span>
    {cell.notes.map(note => <div key={`${note.text}/${note.title ?? ''}`} className="text-xs text-muted-foreground" title={note.title}>
      {note.href != null ? <a href={note.href} aria-label={`${label}: approval waiting ${note.title ?? ''}`}>{note.text}</a> : note.text}
    </div>)}
  </div>
}

/** The line's runs, newest first: its run records, opened in place (Console has no page per run). */
function LaneRuns({ row, p }: { row: Lane; p: ReleaseLanesProps }) {
  const runs = [row.stg, row.prod, row.build].flatMap(name => (name != null ? (p.runs[name]?.data ?? []) : []))
    .sort((a, b) => (b.start_time ?? '').localeCompare(a.start_time ?? '')).slice(0, 6)
  return <details className="col-span-full text-xs">
    <summary className="cursor-pointer text-muted-foreground">{`${row.id} runs · ${runs.length}`}</summary>
    <ul className="m-0 list-none p-0 font-mono-tabular [overflow-wrap:anywhere]">
      {runs.length === 0 ? <li>No run read.</li> : runs.map(run => <li key={run.name}>
        {`${run.name} · ${formatPipelineRunStatus(run)} · started ${ago(run.start_time, p.now) ?? 'at an unrecorded time'} · ${runRevision(run, row.rev)}`}
      </li>)}
    </ul>
  </details>
}

export function ReleaseLanesSection(p: ReleaseLanesProps) {
  const w = p.window
  const rule = p.rule.data?.in_force
  const meta = 'm-0 px-3 pb-1 text-xs text-muted-foreground [overflow-wrap:anywhere]'
  return <OpsSection id="release-lanes" title="Release lanes" bodyPadding="none" overflow="visible"
    description="Each release line: landing, the last successful STG and PROD delivery (what runs now is not confirmed), and the one release window every repository shares.">
    <div className="@container flex min-w-0 flex-col">
      <div data-testid="release-lanes-window" className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2 text-sm">
        <DenseTag variant={w.state === 'held' ? 'warning' : w.state === 'free' ? 'success' : 'neutral'}>{w.state === 'held' || w.state === 'free' ? w.state : 'unknown'}</DenseTag>
        <span className="font-medium">{w.text}</span>
        {w.detail !== '' && <span className="min-w-0 text-muted-foreground [overflow-wrap:anywhere]">{w.detail}</span>}
        {w.state === 'held' && <strong>Blocks landings and releases in every repository until it is released or expires.</strong>}
      </div>
      <p className={`${meta} pb-2`}>
        Landings in flight: <strong className="text-foreground">Unknown</strong>{` — ${IN_FLIGHT_GAP} Default limit ${rule?.limits?.max_in_flight_per_repo ?? 'unknown'} per repository; a repository's own limit shows on its line.`}
      </p>
      <ul aria-label="Release lanes: landing and last successful delivery per environment" className="m-0 list-none p-0">
        {LANES.map(row => <li key={row.id} data-lane={row.id}
          className="grid gap-x-4 gap-y-1 border-t border-[var(--card-border)] px-3 py-2 @3xl:grid-cols-[minmax(0,9rem)_repeat(3,minmax(0,1fr))]">
          <div className="min-w-0"><strong>{row.id}</strong><div className="text-xs text-muted-foreground [overflow-wrap:anywhere]">{row.repos.map(short).join(' · ')}</div></div>
          <LaneCell label="Landing" cell={landingCell(row, p)} />
          {(['stg', 'prod'] as const).map(env => (row.stg != null
            ? <LaneCell key={env} label={`${env.toUpperCase()} · last successful delivery`} cell={deployCell(row, env, p)} />
            : <LaneCell key={env} label={`${env.toUpperCase()} · image in Deployment spec`} cell={imageCell(row, env, p)} />))}
          <LaneRuns row={row} p={p} />
        </li>)}
      </ul>
    </div>
  </OpsSection>
}
