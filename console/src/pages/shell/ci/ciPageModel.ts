import type { CiChange, CiDomainGraph } from '@/api/ciDomains'
import type { CiScorecard, CiScoreItem } from '@/api/ciScorecard'
import type { TimingRun } from '@/api/ciTimings'
import type { UnitsRun } from '@/api/units'
import { modulesRun, type ModulesRun } from './ciViewModel'
import { elapsed, median, reusedBy, runCommit, runStatus, scopeResult, seconds } from './timingModel'

export { median }

/*
 * Work > CI, the first screen and the three views, as values. Everything here is computed from reads the page already
 * makes (the runs of the selected repository, the timings of its pipelines, the release window, the scorecard) and from
 * the two domain routes. A value that cannot be computed from one of them is null and its name goes to the "not
 * measured" list: never a placeholder, never a number nobody read.
 */

// ── Runs ────────────────────────────────────────────────────────────────────

/** A test task is one CI names test-…; the longest of them is what the run's test time is. */
const isTest = (task: string) => task.startsWith('test-')
/** The side a test task is of, for the bar's colour: the front end's, or everything else. */
const isConsole = (task: string) => task.includes('console')

export type PageRun = {
  name: string
  commit: string
  sha: string
  at: string
  pipeline: string
  status: string
  /** true passed, false failed, null not finished. */
  ok: boolean | null
  running: boolean
  full: boolean
  /** Seconds start to completion; for a run in flight, seconds since it was created. Null when neither is known. */
  took: number | null
  /** Seconds before the first task's first step started. */
  lag: number | null
  /** Seconds of everything before the tests: took less lag less the longest test task. */
  setup: number | null
  /** Seconds of the longest test task of each side (task creation to completion). */
  api: number | null
  con: number | null
  /** Which side's tests were longest; 'tests' when the run has no test task. */
  testsBy: 'api' | 'con' | 'tests'
  tests: { task: string; seconds: number | null; status: string }[]
  /** Failed task names. */
  bad: string[]
  /** Tasks running now. */
  nowTasks: string[]
  modules: ModulesRun
  /** What the backend tests covered, from the pipeline's own result; null when that result was not read. */
  scope: 'incremental' | 'full' | 'none' | null
  /** The packages a selective run tested; null when not read. */
  packages: string[] | null
}

const maxOf = (values: (number | undefined)[]): number | null => {
  const known = values.filter((v): v is number => v != null)
  return known.length ? Math.max(...known) : null
}

/** One run as the page draws it. `asOf` is when the list was read; `detail` the same run's timing record when loaded. */
export function pageRun(run: UnitsRun, asOf: string, detail?: TimingRun): PageRun {
  const finished = run.status === 'Succeeded' || run.status === 'Failed'
  const running = run.status === 'Running'
  const sinceCreated = (Date.parse(asOf) - Date.parse(run.created_at)) / 1000
  const took = run.seconds != null ? run.seconds : running && Number.isFinite(sinceCreated) && sinceCreated >= 0 ? Math.round(sinceCreated) : null
  const tests = run.tasks.filter(t => isTest(t.task))
  const first = run.tasks.find(t => t.task === 'clone') ?? run.tasks.filter(t => !isTest(t.task) && t.waited != null).sort((a, b) => (a.waited ?? 0) - (b.waited ?? 0))[0]
  const lag = first?.waited ?? null
  const api = maxOf(tests.filter(t => !isConsole(t.task)).map(t => t.seconds))
  const con = maxOf(tests.filter(t => isConsole(t.task)).map(t => t.seconds))
  const longest = maxOf([api ?? undefined, con ?? undefined])
  const setup = finished && took != null && lag != null && longest != null ? Math.max(0, took - lag - longest) : null
  const scopeValue = detail ? scopeResult(detail, 'api-scope') : undefined
  const scope = scopeValue === 'incremental' || scopeValue === 'full' ? scopeValue : detail && finished ? 'none' : null
  const packages = scope === 'incremental' ? (scopeResult(detail!, 'api-packages') ?? '').split(/\s+/).filter(Boolean) : null
  return {
    name: run.name, commit: run.revision, sha: run.revision.slice(0, 8), at: run.created_at, pipeline: run.pipeline, status: run.status,
    ok: finished ? run.status === 'Succeeded' : null, running, full: run.full, took, lag, setup, api, con,
    testsBy: tests.length === 0 ? 'tests' : (con ?? -1) > (api ?? -1) ? 'con' : 'api',
    tests: tests.map(t => ({ task: t.task, seconds: t.seconds ?? null, status: t.status })),
    bad: run.tasks.filter(t => t.status === 'Failed').map(t => t.task),
    nowTasks: run.tasks.filter(t => t.status === 'Running').map(t => t.task),
    modules: modulesRun(run), scope, packages,
  }
}

/** The runs that tested themselves (a run standing on another run's verdict ran nothing), newest first. */
export function ownRuns(runs: UnitsRun[], asOf: string, details: TimingRun[] = []): PageRun[] {
  const byName = new Map(details.map(d => [d.name, d]))
  return runs.filter(r => (r.tested_by ?? '') === '').map(r => pageRun(r, asOf, byName.get(r.name)))
    .sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
}

const DAY = 24 * 3600_000
/** Median seconds of the finished runs created in the 24 hours before `asOf`; null with none. */
export function median24h(runs: PageRun[], asOf: string): number | null {
  const from = Date.parse(asOf) - DAY
  return median(runs.filter(r => r.ok != null && r.took != null && Date.parse(r.at) >= from).map(r => r.took!))
}

// ── Verdict ─────────────────────────────────────────────────────────────────

export type StageRun = { name: string; commit: string; status: 'Succeeded' | 'Failed' | 'Running' | string; failedTasks: string[] }
export type DeliveryStage = { label: string; runs: StageRun[] }
export type WindowHolder = { who: string; what: string }

export type VerdictInput = {
  /** Own-test runs, newest first. */
  runs: PageRun[]
  asOf: string
  /** Main's head, when read. */
  mainHead?: string
  /** Commits a merge run landed on main, when read. */
  landed?: Set<string>
  /** The delivery stages, each with its runs newest first. */
  deliveries: DeliveryStage[]
  /** The release window's holder; null when nobody holds it; undefined when it was not read. */
  window?: WindowHolder | null
}

export type VerdictLevel = 'Healthy' | 'Degraded' | 'Blocked'
export type Verdict = { level: VerdictLevel; waitsFor: string | null; sentence: string; reasons: string[] }

const LAST = 20
const SLOW_FACTOR = 3

/** A run is on main when it is the daily full run, its commit is main's head, or a merge run landed its commit. */
export const onMain = (run: PageRun, input: Pick<VerdictInput, 'mainHead' | 'landed'>) =>
  run.full || (!!input.mainHead && run.commit === input.mainHead) || (input.landed?.has(run.commit) ?? false)

/**
 * Right now, not three days:
 * Blocked — the newest finished CI run on main is red, or a delivery stage's newest finished run failed (it failed last
 * and nothing newer succeeded). Degraded — any of the last 20 own-test runs is red, or a run has been in flight longer
 * than three times the 24 h median. Otherwise Healthy. `waitsFor` names the step, or the window's holder when the
 * window is held.
 */
export function verdict(input: VerdictInput): Verdict {
  const { runs } = input
  const finished = runs.filter(r => r.ok != null)
  const inFlight = runs.filter(r => r.running)
  const last = finished.slice(0, LAST)
  const red = last.filter(r => r.ok === false).length
  const reasons: string[] = []
  let level: VerdictLevel = 'Healthy'
  let waitsFor: string | null = null

  const main = finished.find(r => onMain(r, input))
  if (main?.ok === false) {
    level = 'Blocked'
    waitsFor = `${main.bad.join(', ') || 'CI'} on ${main.sha}`
    reasons.push(`the newest finished CI run on main (${main.sha}) is red${main.bad.length ? ` in ${main.bad.join(', ')}` : ''}`)
  }
  for (const stage of input.deliveries) {
    const newest = stage.runs.find(r => r.status === 'Succeeded' || r.status === 'Failed')
    if (newest?.status === 'Failed') {
      const step = newest.failedTasks.join(', ')
      if (level !== 'Blocked') waitsFor = `${stage.label}${step ? ` › ${step}` : ''}`
      level = 'Blocked'
      reasons.push(`${stage.label} failed last${step ? ` in ${step}` : ''} (${newest.commit.slice(0, 8) || newest.name}) and nothing newer succeeded`)
    }
  }
  if (level === 'Healthy') {
    if (red > 0) {
      level = 'Degraded'
      reasons.push(`${red} of the last ${last.length} runs ${red === 1 ? 'is' : 'are'} red`)
    }
    const typical = median24h(runs, input.asOf)
    const slow = typical != null ? inFlight.find(r => r.took != null && r.took > SLOW_FACTOR * typical) : undefined
    if (slow) {
      level = 'Degraded'
      waitsFor = `${slow.nowTasks.join(', ') || 'CI'} on ${slow.sha}`
      reasons.push(`${slow.sha} has been in flight ${slow.took} s, more than ${SLOW_FACTOR}× the 24 h median of ${Math.round(typical!)} s`)
    }
  }
  if (input.window) waitsFor = waitsFor ? `${waitsFor}; the release window is held by ${input.window.who}` : `the release window, held by ${input.window.who}`

  const flight = inFlight.length === 0 ? 'Nothing is in CI.'
    : inFlight.length === 1 ? `One change is in CI${inFlight[0].nowTasks.length ? `, at ${inFlight[0].nowTasks.join(', ')}` : ''}.`
      : `${inFlight.length} changes are in CI.`
  const recent = last.length ? ` Of the last ${last.length} runs ${last.length - red} ${last.length - red === 1 ? 'is' : 'are'} green, ${red} red.` : ''
  const window = input.window === undefined ? '' : input.window === null ? ' Nobody holds the release window.' : ` The release window is held by ${input.window.who}${input.window.what ? ` (${input.window.what})` : ''}.`
  return { level, waitsFor, sentence: `${flight}${recent}${window}`, reasons }
}

// ── Pipeline strip ──────────────────────────────────────────────────────────

export type StepState = 'done' | 'now' | 'failed' | 'skip' | 'idle'
export type Step = { label: string; state: StepState; title: string }

/**
 * The CI stage's steps: everything before the tests as one step, then one step a test task. The tasks are the ones
 * the recent runs had; the states are the reference run's (the run in flight, else none: idle).
 */
export function ciSteps(runs: UnitsRun[], reference?: UnitsRun): Step[] {
  const names = new Set<string>()
  for (const run of runs.filter(r => (r.tested_by ?? '') === '').slice(0, LAST)) for (const t of run.tasks) names.add(t.task)
  for (const t of reference?.tasks ?? []) names.add(t.task)
  const before = [...names].filter(n => !isTest(n)).sort()
  const tests = [...names].filter(isTest).sort()
  const stateOf = (tasks: string[]): StepState => {
    if (!reference) return 'idle'
    const mine = reference.tasks.filter(t => tasks.includes(t.task))
    if (mine.length === 0) return reference.status === 'Running' ? 'idle' : 'skip'
    if (mine.some(t => t.status === 'Failed')) return 'failed'
    if (mine.some(t => t.status === 'Running')) return 'now'
    return mine.every(t => t.status === 'Succeeded') ? 'done' : 'idle'
  }
  const left = reference ? modulesRun(reference) : null
  const out: Step[] = []
  if (before.length) out.push({ label: 'setup', state: stateOf(before), title: `clone and scope: ${before.join(', ')}` })
  for (const task of tests) {
    const state = stateOf([task])
    // A side its change did not reach is skipped, not waiting: the scope task said so.
    const skipped = state === 'idle' && left?.kind === 'some' && !left.names.some(n => task.includes(n)) && reference!.tasks.some(t => isTest(t.task))
    out.push({ label: task.replace(/^test-/, ''), state: skipped ? 'skip' : state, title: task })
  }
  return out
}

const stageStatus = (run: TimingRun) => runStatus(run)

/**
 * The phase a delivery task belongs to, by what its name says it does. On STG, 2026-11-16, one step a task gave the
 * strip ten chips a stage whose names were cut on both sides ("e and sco", "t-api-sto"); a phase is a word.
 */
export function phaseOf(task: string): string {
  const rules: Array<[RegExp, string]> = [
    [/^clone|mirror/, 'clone'], [/freeze|preflight|release-check|check-source/, 'check'], [/^build/, 'build'],
    [/migrat|schema/, 'migrate'], [/promote/, 'promote'], [/gitops|sync/, 'sync'], [/rollout/, 'rollout'], [/^land$|merge/, 'merge'],
  ]
  return rules.find(([re]) => re.test(task))?.[1] ?? task
}

/** A stage's steps: the phases of its newest run's tasks, in the order they first started; lit only while that run is in flight. */
export function stageSteps(runs: TimingRun[]): Step[] {
  const newest = runs[0]
  if (!newest) return []
  const live = stageStatus(newest) === 'Running'
  const phases = new Map<string, { tasks: string[]; statuses: string[] }>()
  for (const t of [...newest.tasks].sort((a, b) => Date.parse(a.created_at ?? '') - Date.parse(b.created_at ?? '') || a.task.localeCompare(b.task))) {
    const phase = phases.get(phaseOf(t.task)) ?? { tasks: [], statuses: [] }
    phase.tasks.push(t.task)
    phase.statuses.push(t.status.toLowerCase())
    phases.set(phaseOf(t.task), phase)
  }
  return [...phases].map(([label, { tasks, statuses }]) => {
    const state: StepState = !live ? 'idle' : statuses.includes('failed') ? 'failed' : statuses.includes('running') ? 'now'
      : statuses.every(x => x === 'succeeded') ? 'done' : 'idle'
    return { label, state, title: tasks.join(', ') }
  })
}

export function stageRuns(runs: TimingRun[]): StageRun[] {
  return runs.map(r => ({ name: r.name, commit: runCommit(r), status: stageStatus(r), failedTasks: r.tasks.filter(t => t.status.toLowerCase() === 'failed').map(t => t.task) }))
}

/** Median seconds of a stage's succeeded runs created in the 24 hours before the read; null with none. */
export function stageMedian(runs: TimingRun[], asOf: string): { seconds: number | null; samples: number } {
  const from = Date.parse(asOf) - DAY
  const values = runs.filter(r => stageStatus(r) === 'Succeeded' && Date.parse(r.created_at) >= from).map(elapsed).filter((n): n is number => n != null)
  return { seconds: median(values), samples: values.length }
}

/** Commits a succeeded merge run landed. */
export const landedCommits = (lands: TimingRun[]) => new Set(lands.filter(r => stageStatus(r) === 'Succeeded').map(runCommit).filter(Boolean))

/**
 * Seconds from a merge completing to the delivery of the same commit starting, over the deliveries loaded: how long
 * after main moves a delivery starts. Null when no delivery has its merge among the loaded records.
 */
export function startsAfterMain(lands: TimingRun[], deliveries: TimingRun[]): { seconds: number | null; samples: number } {
  const landedAt = new Map<string, string>()
  for (const land of lands) if (stageStatus(land) === 'Succeeded' && land.completion_time && runCommit(land)) landedAt.set(runCommit(land), land.completion_time)
  const seen = new Set<string>()
  const gaps: number[] = []
  // Oldest first, so a commit's first delivery is the one counted.
  for (const d of [...deliveries].sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at))) {
    const commit = runCommit(d)
    if (!commit || seen.has(commit) || !landedAt.has(commit)) continue
    seen.add(commit)
    const gap = seconds(landedAt.get(commit), d.start_time ?? d.created_at)
    if (gap != null) gaps.push(gap)
  }
  return { seconds: median(gaps), samples: gaps.length }
}

/** The run of a stage that is in flight, newest first. */
export const inFlightOf = (runs: TimingRun[]) => runs.find(r => stageStatus(r) === 'Running')
/** The newest succeeded run of a stage. */
export const lastSucceeded = (runs: TimingRun[]) => runs.find(r => stageStatus(r) === 'Succeeded' && !reusedBy(r))

export type SelectiveFact = { selective: number; known: number; selectiveMedian: number | null; fullMedian: number | null }
/** How many of the runs whose scope was read tested only what changed, and the two medians. Null when no scope was read. */
export function selectiveFact(runs: PageRun[]): SelectiveFact | null {
  const known = runs.filter(r => r.ok != null && (r.scope === 'incremental' || r.scope === 'full'))
  if (known.length === 0) return null
  const took = (scope: string) => median(known.filter(r => r.scope === scope && r.took != null).map(r => r.took!))
  return { selective: known.filter(r => r.scope === 'incremental').length, known: known.length, selectiveMedian: took('incremental'), fullMedian: took('full') }
}

/** How many of the finished runs that reported their modules left one out, and of how many. Null when none reported. */
export function leftOutFact(runs: PageRun[]): { leftOut: number; reported: number } | null {
  const reported = runs.filter(r => r.ok != null && r.modules.kind !== 'not_reported')
  return reported.length ? { leftOut: reported.filter(r => r.modules.kind === 'some').length, reported: reported.length } : null
}

// ── Domains ─────────────────────────────────────────────────────────────────

export const PLACE_LABEL: Record<string, string> = { shared: 'shared packages', console: 'console (frontend)', repo: 'repository files' }
export const EXTRA_PLACES = ['shared', 'console', 'repo']
export const placeLabel = (id: string) => PLACE_LABEL[id] ?? id

export type ChangeMap = Map<string, CiChange>
export const changeMap = (changes: CiChange[] | undefined): ChangeMap => new Map((changes ?? []).map(c => [c.commit, c]))

/** A run counts for a domain when its commit was read and changed a file of it. With no domain, every run does. */
export function touches(run: PageRun, domain: string, changes: ChangeMap): boolean {
  if (domain === '') return true
  const change = changes.get(run.commit)
  return !!change?.read && change.domains[domain] != null
}

/** The runs that count for a domain, newest first. */
export const touchedRuns = (runs: PageRun[], domain: string, changes: ChangeMap) => runs.filter(r => touches(r, domain, changes))

export type DomainCard = { id: string; label: string; runs: PageRun[]; red: number; median: number | null; selective: number }

/** One card a domain, then the three places that are not a domain. Runs are oldest first. */
export function domainCards(graph: CiDomainGraph, runs: PageRun[], changes: ChangeMap): DomainCard[] {
  return [...graph.domains.map(d => d.id), ...EXTRA_PLACES].map(id => {
    const mine = runs.filter(r => touches(r, id, changes)).reverse()
    return {
      id, label: placeLabel(id), runs: mine, red: mine.filter(r => r.ok === false).length,
      median: median(mine.filter(r => r.ok != null && r.took != null).map(r => r.took!)), selective: mine.filter(r => r.scope === 'incremental').length,
    }
  })
}

export type DomainPanel = {
  dependsOn: { id: string; imports: number; wrongWay: boolean }[]
  dependedOnBy: { id: string; imports: number; wrongWay: boolean }[]
  packages: number
  shared: number
}
export function domainPanel(graph: CiDomainGraph, domain: string): DomainPanel | null {
  const node = graph.domains.find(d => d.id === domain)
  if (!node) return null
  return {
    dependsOn: graph.edges.filter(e => e.from === domain).map(e => ({ id: e.to, imports: e.imports, wrongWay: e.wrong_way })),
    dependedOnBy: graph.edges.filter(e => e.to === domain).map(e => ({ id: e.from, imports: e.imports, wrongWay: e.wrong_way })),
    packages: node.packages, shared: graph.shared_packages.length,
  }
}

export type GraphNode = { id: string; x: number; y: number; width: number; height: number; packages: number }
export const NODE_W = 168
export const NODE_H = 58
const COLS = 4
const GAP_X = 64
const GAP_Y = 96

/**
 * Where each domain is drawn: the base (the domain most others import) wide in the middle, the rest in two rows, one
 * above it and one below, in the table's order. An arrow to the base then crosses no other node. With no base the
 * domains are rows of four. At phone width the rows are two columns wide (an arrow from an outer row then passes
 * the row between it and the base).
 */
export function graphLayout(graph: CiDomainGraph, narrow = false): GraphNode[] {
  const inbound = new Map<string, number>()
  for (const e of graph.edges) if (!e.wrong_way) inbound.set(e.to, (inbound.get(e.to) ?? 0) + 1)
  const base = [...graph.domains].sort((a, b) => (inbound.get(b.id) ?? 0) - (inbound.get(a.id) ?? 0))[0]
  const hasBase = base != null && (inbound.get(base.id) ?? 0) >= 2
  const rest = graph.domains.filter(d => !hasBase || d.id !== base.id)
  // At phone width two columns, so a node keeps a readable size; the base then has two rows on each side.
  const above = hasBase ? Math.ceil(rest.length / 2) : rest.length
  const cols = Math.max(1, narrow ? Math.min(2, rest.length) : hasBase ? above : Math.min(COLS, rest.length))
  const width = cols * NODE_W + (cols - 1) * GAP_X
  const pitch = NODE_H + GAP_Y
  const rowsAbove = Math.ceil(above / cols)
  return [
    ...rest.map((d, i) => {
      const below = i >= above
      const k = below ? i - above : i
      const row = Math.floor(k / cols) + (below ? rowsAbove + 1 : 0)
      return { id: d.id, x: (k % cols) * (NODE_W + GAP_X), y: row * pitch, width: NODE_W, height: NODE_H, packages: d.packages }
    }),
    ...(hasBase ? [{ id: base.id, x: 0, y: rowsAbove * pitch, width, height: NODE_H, packages: base.packages }] : []),
  ]
}

/** The drawing's size, for a frame of the same shape (no empty band beside or under the graph). */
export function graphSize(nodes: GraphNode[]): { width: number; height: number } {
  return { width: Math.max(1, ...nodes.map(n => n.x + n.width)), height: Math.max(1, ...nodes.map(n => n.y + n.height)) }
}

/**
 * Which sides an arrow leaves and enters by, and its shape. Between rows it is straight, top to bottom. In one row
 * it is straight between neighbours; past a neighbour it arcs outside the row (above the first row, below any other)
 * so that it does not run through the node between.
 */
export function edgeSides(from: GraphNode, to: GraphNode): [string, string, 'straight' | 'default'] {
  if (to.y > from.y) return ['bottom', 'top', 'straight']
  if (to.y < from.y) return ['top', 'bottom', 'straight']
  if (Math.abs(to.x - from.x) <= NODE_W + GAP_X) return to.x > from.x ? ['right', 'left', 'straight'] : ['left', 'right', 'straight']
  return from.y === 0 ? ['top', 'top', 'default'] : ['bottom', 'bottom', 'default']
}

// ── Timeline ────────────────────────────────────────────────────────────────

/** How many bars By time draws before "show all". */
export const TIMELINE_DEFAULT = 40

/** Chicago wall time, as the Owner reads it. */
export const hhmm = (at: string) => new Date(at).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', timeZone: 'America/Chicago' })

/** The drawing's own coordinates. At phone width the drawing is 360 units wide, so a 10-unit label stays about 10 px. */
export function timelineGeometry(narrow: boolean, maxSeconds: number) {
  const width = narrow ? 360 : 1120
  const rowH = narrow ? 11 : 9
  const gap = narrow ? 4 : 3
  const left = 40
  // Room at the right for the longest label a bar carries (a failed task's name).
  const right = narrow ? 112 : 150
  const scale = (width - left - right) / Math.max(1, maxSeconds)
  // Tick labels ("960 s") are about 30 units wide: the step is the first that keeps them apart.
  const step = [30, 60, 120, 300, 600, 1200, 3600].find(s => s * scale >= 44) ?? 3600
  return { width, rowH, gap, left, right, scale, step }
}

// ── Scores ──────────────────────────────────────────────────────────────────

export type ScoreRow = { id: string; name: string; items: CiScoreItem[] }
export function scoreRows(card: CiScorecard): ScoreRow[] {
  return [...card.repos.map(r => ({ id: r.repo, name: r.repo, items: r.items })), { id: 'provider', name: 'CI itself', items: card.provider.items }]
}

// ── Not measured ────────────────────────────────────────────────────────────

export type NotMeasuredInput = {
  repo: string
  runs: PageRun[]
  /** undefined: the changes were not read at all (the reason is in `unread`). */
  changes?: ChangeMap
  graph?: CiDomainGraph
  scorecard?: CiScorecard
  /** Whether the delivery stages of this repository are read. */
  stages: boolean
  /** Reads that failed, as "what: why". */
  unread: string[]
}

/** The facts of the approved sketch that no read of the page answers. They are named here and drawn nowhere. */
const NO_SOURCE = [
  'Reconcile: whether main\'s full run agreed with the selective runs before it (the full run\'s notes carry no count)',
  'Queue: how many deliveries waited behind another one, and how long',
  'Whether the signed release policy covers the next release, and whether a card waits for the Owner',
  'Which domain a failing test belongs to',
  'Dependencies between repositories other than the pins a repository declares',
]

/** Everything the page could not measure, in one list: what it computed as missing, then what the API reports. */
export function notMeasured(input: NotMeasuredInput): string[] {
  const out: string[] = []
  const { runs } = input
  if (input.changes) {
    const gone = runs.filter(r => input.changes!.get(r.commit)?.read === false).length
    const unasked = runs.filter(r => !input.changes!.has(r.commit)).length
    if (gone > 0) out.push(`What changed in ${gone} of ${runs.length} runs: their commits are not in the repository (they never reached a kept branch) or could not be read, so they appear By time but under no domain`)
    if (unasked > 0) out.push(`What changed in ${unasked} of ${runs.length} runs: not asked (one read names at most 120 commits)`)
  }
  const finished = runs.filter(r => r.ok != null)
  const noScope = finished.filter(r => r.scope == null).length
  if (noScope > 0) out.push(`Whether ${noScope} of ${finished.length} finished runs were selective, and which packages they tested: only the newest 60 runs of the pipeline are read in detail`)
  const noSplit = finished.filter(r => r.lag == null || r.setup == null).length
  if (noSplit > 0) out.push(`The phases of ${noSplit} of ${finished.length} finished runs (no task reported its start, or the run has no test task): their bars are one colour`)
  if (input.graph && !input.graph.measured) out.push(`Domains of ${input.repo}: ${input.graph.note || 'not measured'}`)
  if (!input.stages) out.push(`Land, STG and PROD of ${input.repo}: the delivery timings are read for bifrost-platform only`)
  out.push(...NO_SOURCE)
  out.push(...input.unread)
  if (input.scorecard) {
    for (const row of scoreRows(input.scorecard)) for (const item of row.items) {
      if (item.verdict === 'unmeasured') out.push(`Score "${item.label}" of ${row.name}: ${item.value}`)
    }
  }
  return out
}
