import { DenseTag } from '@bifrost/ui'
import { TAG_VARIANT } from '@/lib/shell/stateWords'
import type { ApprovalItem } from '@/api/approvals'
import type { DeliveryPipelineRunView, DeliveryPipelineView } from '@/api/deliveryTypes'
import type { ReleaseRecord, ReleaseRule, RunningImageCell } from '@/api/releases'
import { OpsSection } from '@/components/layout/OpsSection'
import { formatPipelineRunStatus, isPipelineRunFailed, isPipelineRunRunning, isPipelineRunSucceeded } from '@/lib/delivery/pipelineRunAskPack'
import { RELEASE_RECORD_LIMIT, approvalHref, formatBuiltFrom, formatBuiltFromTitle, formatDuration, formatInstant, shortSha,
  type ReleaseWindowView, releaseWindowWord } from '@/pages/shell/releases/releaseView'


/** One read: `data` once answered, `error` once failed, neither while it is read. */
export type LaneRead<T> = { data?: T; error: string | null }

export type ReleaseLanesProps = {
  now: number
  hasToken: boolean
  window: ReleaseWindowView
  records: LaneRead<ReleaseRecord[]>
  /** config/release-rules.yaml as the platform loaded it (GET /api/v1/releases status.rule_list): the lines come from it. */
  rules: LaneRead<ReleaseRule[]>
  /** The release pipelines delivery/pipelines lists, with the repositories each clones. */
  pipelines: LaneRead<DeliveryPipelineView[]>
  /** By pipeline; a pipeline missing here was not read. */
  runs: Record<string, LaneRead<DeliveryPipelineRunView[]>>
  images: LaneRead<RunningImageCell[]>
  approvals: ApprovalItem[]
}

/**
 * One release line, read from release-rules.yaml (OWNER_LAW #30, 2026-11-17: no hand-kept list): `stg` and `prod` are the
 * pipelines whose runs deploy that environment, `prefixes` the run-name rules of an environment (runs no pipeline label
 * finds), `builds` the image builds a pin or GitOps sync deploys later. A line with neither STG nor PROD shows live images.
 */
type Line = { id: string; stg?: string; prod?: string; prefixes: { env: string; prefix: string }[]; builds: string[] }

function releaseLines(rules: ReleaseRule[]): Line[] {
  const lines: Line[] = []
  for (const rule of rules) {
    let line = lines.find(l => l.id === rule.lane)
    if (line == null) lines.push(line = { id: rule.lane, prefixes: [], builds: [] })
    const pipeline = rule.match.pipeline ?? ''
    const env = rule.env === 'stg' || rule.env === 'prod' ? rule.env : null
    if (rule.deploys && env != null) {
      if (pipeline !== '' && line[env] == null) line[env] = pipeline
      if ((rule.match.name_prefix ?? '') !== '') line.prefixes.push({ env, prefix: rule.match.name_prefix! })
    } else if (pipeline !== '' && !line.builds.includes(pipeline)) line.builds.push(pipeline)
  }
  return lines
}

const deploys = (line: Line) => line.stg != null || line.prod != null
/** The pipelines whose results a line's cells show. */
const shown = (line: Line) => (deploys(line) ? [line.stg, line.prod].filter((n): n is string => n != null) : line.builds)

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

/** The repository a pipeline's `revision` parameter clones (delivery/pipelines revision_repo), short; undefined when unknown. */
const revOf = (p: ReleaseLanesProps, pipeline: string) => {
  const repo = p.pipelines.data?.find(x => x.name === pipeline)?.revision_repo
  return repo != null && repo !== '' ? short(repo) : undefined
}

/** A run's revision, named by the repository its `revision` parameter clones when there is one. */
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
function shipped(p: ReleaseLanesProps, pipeline: string, keep: (r: ReleaseRecord) => boolean): Cell | null {
  const rev = revOf(p, pipeline)
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
function inFlight(pipeline: string | undefined, verb: string, p: ReleaseLanesProps): Note[] {
  const rev = revOf(p, pipeline ?? '')
  const running = (p.runs[pipeline ?? '']?.data ?? []).filter(isPipelineRunRunning)
    .map(run => ({ text: `${verb} ${runRevision(run, rev)} · started ${ago(run.start_time, p.now) ?? 'at an unrecorded time'}`, title: run.name }))
  const waiting = p.approvals.filter(a => a.action === 'start_pipeline_run' && a.params?.name === pipeline)
    .map(a => ({ text: 'Approval waiting ›', title: a.id, href: approvalHref(a.id) }))
  return [...running, ...waiting]
}

/** The last successful delivery. deliver-* roll out before they verify, so a later run that ended unsuccessfully may have rolled out. */
function deployCell(row: Line, env: 'stg' | 'prod', p: ReleaseLanesProps): Cell {
  const pipeline = row[env]
  if (pipeline == null) return { text: `No ${env.toUpperCase()}`, notes: [{ text: `release-rules.yaml has no deploying ${env} rule for ${row.id}` }] }
  const cell = shipped(p, pipeline, r => r.deploys && r.lane === row.id && r.env === env) ?? missing(p, pipeline)
  const shownAt = cell.at ?? Number.NaN
  const ended = (r: DeliveryPipelineRunView) => r.completion_time ?? r.start_time
  const failed = (p.runs[pipeline]?.data ?? []).filter(isPipelineRunFailed).filter(r => !Number.isFinite(shownAt) || rank(ended(r)) > shownAt)
    .sort((a, b) => rank(ended(b)) - rank(ended(a)))[0]
  const risk = failed == null ? [] : [{ title: failed.name,
    text: `${Number.isFinite(shownAt) ? 'newer ' : ''}run ${failed.name} ${formatPipelineRunStatus(failed)} at ${when(ended(failed), p.now)} — it may have rolled out; what runs now is not confirmed` }]
  // Run-name rules (release-rules.yaml name_prefix) find runs this pipeline's run list does not hold.
  const prefixed = row.prefixes.filter(x => x.env === env).map(x => ({ text: `runs named ${x.prefix}* are releases too but not in this run list: `
    + 'one shows here only once the recorder (every 5 minutes) has recorded it, so one in flight or one that failed its checks after rolling out does not' }))
  return { ...cell, notes: [...cell.notes, ...risk, ...inFlight(pipeline, 'Delivering', p), ...prefixed] }
}

/** Lines without a deploying pipeline: the live images (running-images), and under PROD each image build's last success. */
function imageCell(row: Line, env: 'stg' | 'prod', p: ReleaseLanesProps): Cell {
  const live = p.images.data?.find(c => c.lane === row.id && c.env === env)
  const cell = p.images.error != null ? unknown(`running-images: ${p.images.error}`) : p.images.data == null ? READING
    : live == null ? unknown(`running-images has no ${env} cell for ${row.id}`)
    : live.absent === true ? { text: live.text, notes: [] }
    : { mono: true, text: live.text, title: live.title, notes: [{ text: 'image in the Deployment spec; rollout state not read' }] }
  if (env === 'stg') return cell
  const built = row.builds.flatMap(build => {
    const last = shipped(p, build, r => r.pipeline === build)
    return [...(last == null ? [] : [{ text: `last ${short(build)} ${last.text} · ${last.notes.map(n => n.text).join('; ')}`, title: last.title }]),
      ...inFlight(build, 'Building', p)]
  })
  return { ...cell, notes: [...cell.notes, ...built] }
}

/** The repositories a line's pipelines clone (delivery/pipelines repos), or why they are not known. */
function lineRepos(row: Line, p: ReleaseLanesProps): string {
  if (p.pipelines.error != null) return `repositories unknown: pipelines: ${p.pipelines.error}`
  if (p.pipelines.data == null) return 'repositories: reading…'
  const listed = shown(row).map(name => p.pipelines.data!.find(x => x.name === name)).filter((x): x is DeliveryPipelineView => x != null)
  const repos = [...new Set(listed.flatMap(x => x.repos ?? []))]
  if (listed.length === 0) return `repositories unknown: ${shown(row).join(', ') || 'no pipeline'} not listed`
  return repos.length === 0 ? 'repositories unknown: its pipelines name no clone' : repos.map(short).join(' · ')
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
function LaneRuns({ row, p }: { row: Line; p: ReleaseLanesProps }) {
  const runs = [row.stg, row.prod, ...row.builds].flatMap(name => (name != null ? (p.runs[name]?.data ?? []) : []))
    .sort((a, b) => (b.start_time ?? '').localeCompare(a.start_time ?? '')).slice(0, 6)
  return <details className="col-span-full text-xs">
    <summary className="cursor-pointer text-muted-foreground">{`${row.id} runs · ${runs.length}`}</summary>
    <ul className="m-0 list-none p-0 font-mono-tabular [overflow-wrap:anywhere]">
      {runs.length === 0 ? <li>No run read.</li> : runs.map(run => <li key={run.name}>
        {`${run.name} · ${formatPipelineRunStatus(run)} · started ${ago(run.start_time, p.now) ?? 'at an unrecorded time'} · ${runRevision(run, revOf(p, run.pipeline))}`}
      </li>)}
    </ul>
  </details>
}

/** Release pipelines no rule names: their runs are never recorded, so which line and environment they ship is unknown. */
function Unruled({ lines, p }: { lines: Line[]; p: ReleaseLanesProps }) {
  const named = new Set(lines.flatMap(l => [l.stg, l.prod, ...l.builds]))
  const rest = (p.pipelines.data ?? []).map(x => x.name).filter(name => !named.has(name)).sort()
  if (rest.length === 0) return null
  return <li data-lane="no-rule" className="border-t border-[var(--card-border)] px-3 py-2 text-sm">
    <strong>No release rule</strong>
    <div className="text-xs text-muted-foreground">release-rules.yaml names no rule for these pipelines: their runs are not recorded as releases, and their line and environment are unknown.</div>
    <ul className="m-0 list-none p-0 text-xs font-mono-tabular [overflow-wrap:anywhere]">
      {rest.map(name => {
        const last = shipped(p, name, () => false)
        return <li key={name}>{`${name} · ${last == null ? (unreadWhy(p, name) != null ? `runs not read (${unreadWhy(p, name)})` : 'no successful run read') : `last success ${last.text} · ${last.notes[0]?.text ?? ''}`}`}</li>
      })}
    </ul>
  </li>
}

export function ReleaseLanesSection(p: ReleaseLanesProps) {
  const w = p.window
  const lines = p.rules.data == null ? [] : releaseLines(p.rules.data)
  return <OpsSection id="release-lanes" title="Release lanes" bodyPadding="none" overflow="visible"
    description="Each release line: the last successful STG and PROD delivery (what runs now is not confirmed), and the one release window every repository shares.">
    <div className="@container flex min-w-0 flex-col">
      <div data-testid="release-lanes-window" className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2 text-sm">
        <DenseTag variant={TAG_VARIANT[releaseWindowWord(w.state)]} title={w.state}>{releaseWindowWord(w.state)}</DenseTag>
        {w.text.toLowerCase() !== releaseWindowWord(w.state) && <span className="font-medium">{w.text}</span>}
        {w.detail !== '' && <span className="min-w-0 text-muted-foreground [overflow-wrap:anywhere]">{w.detail}</span>}
        {w.state === 'held' && <strong>Blocks releases in every repository until it is released or expires.</strong>}
      </div>
      <ul aria-label="Release lanes: last successful delivery per environment" className="m-0 list-none p-0">
        {p.rules.data == null && <li data-lane="unread" className="border-t border-[var(--card-border)] px-3 py-2 text-sm">
          {p.rules.error != null ? `Unknown: the release lines come from release-rules.yaml, which was not read (${p.rules.error})` : 'Reading…'}</li>}
        {p.rules.data?.length === 0 && <li data-lane="none" className="border-t border-[var(--card-border)] px-3 py-2 text-sm">release-rules.yaml has no rules: no release line.</li>}
        {lines.map(row => <li key={row.id} data-lane={row.id}
          className="grid gap-x-4 gap-y-1 border-t border-[var(--card-border)] px-3 py-2 @3xl:grid-cols-[minmax(0,9rem)_repeat(2,minmax(0,1fr))]">
          <div className="min-w-0"><strong>{row.id}</strong><div className="text-xs text-muted-foreground [overflow-wrap:anywhere]">{lineRepos(row, p)}</div></div>
          {(['stg', 'prod'] as const).map(env => (deploys(row)
            ? <LaneCell key={env} label={`${env.toUpperCase()} · last successful delivery`} cell={deployCell(row, env, p)} />
            : <LaneCell key={env} label={`${env.toUpperCase()} · image in Deployment spec`} cell={imageCell(row, env, p)} />))}
          <LaneRuns row={row} p={p} />
        </li>)}
        {p.rules.data != null && <Unruled lines={lines} p={p} />}
      </ul>
    </div>
  </OpsSection>
}
