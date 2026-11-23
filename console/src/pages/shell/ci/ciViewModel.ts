import type { FullCiNote, FullCiNotes, UnitsConfig, UnitsModule, UnitsPin, UnitsRun } from '@/api/units'
import type { Block, Word } from '@bifrost/ui/city/cards'
import { statusOfWord } from '@bifrost/ui/city/world'
import { formatInstant, shortSha } from '@/pages/shell/releases/releaseView'

/*
 * Work > CI, as rows and strings. Everything here is computed from what the units routes answer; no project, module
 * or directory is named in this file.
 */

/** The two columns the platform does not answer yet: said, not left blank and not guessed. */
export const NOT_DECLARED_YET = 'not declared yet'
const UNDECLARED_TEXT = 'Not declared: everything runs'
const UNREADABLE_TEXT = 'Declaration does not read'
export const NO_RUNS_TEXT = 'No CI run of this project is retained'
export const NO_FULL_RUN_TEXT = 'No full run has been recorded yet'
const NO_SUSPECT_TEXT = 'No landed commit left a module out'

const sha8 = (commit: string | undefined) => (commit ?? '').trim().slice(0, 8)

// ── Configuration ───────────────────────────────────────────────────────────

export type ConfigState = 'declared' | 'undeclared' | 'unreadable'

/** declared false: no declaration. declared true with a note and no module: the declaration does not read. */
function configState(config: UnitsConfig): ConfigState {
  if (!config.declared) return 'undeclared'
  return (config.note ?? '') !== '' && config.modules.length === 0 ? 'unreadable' : 'declared'
}

const CONFIG_STATE_TEXT: Record<ConfigState, string> = {
  declared: 'declared', undeclared: UNDECLARED_TEXT, unreadable: UNREADABLE_TEXT,
}

/** The repositories this one is tested with, each at its commit. */
const pinsText = (pins: UnitsPin[]) => pins.map(pin => `${pin.repo}@${sha8(pin.commit)}`).join(', ')

export type ConfigModuleRow = {
  name: string
  kind: string
  root: string
  tasks: string
  units: number
  reads: string
  everyChange: string
  contracts: string
  shipsAlone: string
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`

function moduleRow(module: UnitsModule): ConfigModuleRow {
  return {
    name: module.name,
    kind: module.kind ?? '—',
    root: module.root,
    tasks: module.tasks.map(t => `${t.task} ×${t.units}${t.shards != null ? ` (${plural(t.shards, 'shard')})` : ''}`).join(', '),
    units: module.units,
    reads: module.needs.length > 0 ? module.needs.join(', ') : 'its own tree only',
    everyChange: `${module.whole_repository} of ${plural(module.units, 'unit')}`,
    contracts: NOT_DECLARED_YET,
    shipsAlone: NOT_DECLARED_YET,
  }
}

export type ConfigView = { state: ConfigState; stateText: string; note: string; pins: string; modules: ConfigModuleRow[] }

export function configView(config: UnitsConfig): ConfigView {
  const state = configState(config)
  return {
    state,
    stateText: CONFIG_STATE_TEXT[state],
    note: config.note ?? '',
    pins: pinsText(config.pins),
    // A project with no declaration has no module rows: all of it runs.
    modules: state === 'declared' ? config.modules.map(moduleRow) : [],
  }
}

// ── Runs ────────────────────────────────────────────────────────────────────

export type ModulesRun = { kind: 'not_reported' } | { kind: 'all' } | { kind: 'some'; names: string[] }

/** "*" is every module; otherwise the names, each between spaces. Not asked is not reported: neither all nor none. */
export function modulesRun(run: Pick<UnitsRun, 'asked' | 'run_modules'>): ModulesRun {
  if (!run.asked || run.run_modules == null) return { kind: 'not_reported' }
  if (run.run_modules.trim() === '*') return { kind: 'all' }
  return { kind: 'some', names: run.run_modules.split(/\s+/).filter(Boolean) }
}

export function modulesRunText(run: Pick<UnitsRun, 'asked' | 'run_modules'>): string {
  const ran = modulesRun(run)
  if (ran.kind === 'not_reported') return 'not reported'
  if (ran.kind === 'all') return 'all'
  return ran.names.length > 0 ? ran.names.join(', ') : 'no module'
}

/** A daily full run, a run standing on another run's verdict, or a run of its own. */
export function runKindText(run: Pick<UnitsRun, 'full' | 'tested_by'>): string {
  if (run.full) return 'full'
  if ((run.tested_by ?? '') !== '') return `reused ${run.tested_by}`
  return 'own run'
}

export type StatusVariant = 'success' | 'danger' | 'info' | 'neutral'
const statusVariant = (status: string): StatusVariant =>
  status === 'Succeeded' ? 'success' : status === 'Failed' ? 'danger' : status === 'Running' ? 'info' : 'neutral'

/** 215 → "3m 35s". */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const rest = s % 60
  if (h > 0) return `${h}h ${m}m ${rest}s`
  if (m > 0) return `${m}m ${rest}s`
  return `${rest}s`
}

/** No duration is a run that has not ended; one that is not running either has a duration nobody read. */
export function tookText(run: Pick<UnitsRun, 'seconds' | 'status'>): string {
  if (run.seconds != null) return formatDuration(run.seconds)
  return run.status === 'Running' ? 'running' : 'not known'
}

export type TaskCell = { text: string; failed: boolean }
export type TasksView = { tasks: TaskCell[]; waited: string | null }

export function tasksView(run: Pick<UnitsRun, 'tasks'>): TasksView {
  const tasks = run.tasks.map(t => {
    const failed = t.status === 'Failed'
    const took = t.seconds != null ? `${Math.round(t.seconds)}s` : t.status === 'Running' ? 'running' : t.status.toLowerCase()
    return { text: `${t.task} ${took}${failed ? ' (failed)' : ''}`, failed }
  })
  const waits = run.tasks.map(t => t.waited).filter((w): w is number => w != null)
  return { tasks, waited: waits.length > 0 ? `waited ≤ ${Math.round(Math.max(...waits))}s` : null }
}

export type RunRow = {
  name: string
  createdAt: string
  revision: string
  kind: string
  status: string
  statusVariant: StatusVariant
  modules: string
  why: string
  took: string
  tasks: TasksView
}

export function runRow(run: UnitsRun): RunRow {
  const reused = (run.tested_by ?? '') !== ''
  return {
    name: run.name,
    createdAt: run.created_at,
    revision: run.revision,
    kind: runKindText(run),
    status: run.status,
    statusVariant: statusVariant(run.status),
    // A run that reused another run's verdict ran nothing itself: what its own
    // changes task answered ("*", it sees a commit already on main) is not what ran.
    modules: reused ? `none itself: stands on ${run.tested_by}` : modulesRunText(run),
    why: reused ? '' : (run.scope_why ?? ''),
    took: tookText(run),
    tasks: tasksView(run),
  }
}

/**
 * CI → Runs as one Ledger table (Ledger line, 2026-11-23: up to 100 retained runs, a few hours of platform, drawn
 * whole with no way to find the failed ones; now filtered, sorted and 50 a page). The run name opens the run as the
 * page's one selection. Status says the engine's seven words with the run's own word beside it ("fail · Failed");
 * a failed task carries fail; a scope the pipeline does not report stays unknown.
 */
export function runsLedgerTable(rows: RunRow[]): Extract<Block, { kind: 'table' }> {
  return {
    kind: 'table',
    head: ['Run', 'When', 'Commit', 'Kind', 'Status', 'Modules run', 'Why', 'Took', 'Tasks'].map(label => ({ label })),
    rows: rows.map(row => {
      const word = statusOfWord(row.status)
      // Failed tasks first, each on its own line in fail; the passing ones on one line, as the dense table listed them.
      const failed = row.tasks.tasks.filter(task => task.failed)
      const passing = row.tasks.tasks.filter(task => !task.failed).map(task => task.text)
      const tasks: Word[] = failed.map(task => ({ text: task.text, tone: 'fail' as const }))
      if (passing.length > 0) tasks.push(passing.join(', '))
      if (row.tasks.waited != null) tasks.push({ text: row.tasks.waited, tone: 'pending' })
      return {
        key: row.name,
        open: row.name,
        cells: [
          row.name,
          formatInstant(row.createdAt),
          { text: shortSha(row.revision), title: row.revision },
          row.kind,
          { text: word === row.status.toLowerCase() ? word : `${word} · ${row.status}`, tone: word, title: row.status },
          row.modules === 'not reported' ? { text: row.modules, tone: 'unknown' } : row.modules,
          row.why,
          row.took,
          { lines: tasks, empty: 'none reported' },
        ],
      }
    }),
    empty: NO_RUNS_TEXT,
  }
}

// ── Daily full run ──────────────────────────────────────────────────────────

export type FullCiRow = {
  at: string
  text: string
  /** A red full run: the two columns below are about it. */
  red: boolean
  failedTasks: string[]
  suspects: string[]
}

function fullCiRow(note: FullCiNote): FullCiRow {
  const detail = note.full_ci ?? {}
  const red = typeof detail.run === 'string' && detail.run !== ''
  if (!red) return { at: note.at, text: note.text, red, failedTasks: [], suspects: [] }
  const failedTasks = (detail.failed_tasks ?? []).length > 0 ? [(detail.failed_tasks ?? []).join(', ')] : ['no failed task was read']
  const failedTests = (detail.failed_key ?? '').split(/\s+/).filter(Boolean)
  if (failedTests.length > 0) failedTasks.push(`failed tests: ${failedTests.join(', ')}`)
  const suspects = (detail.suspects ?? []).map(s => `${sha8(s.commit)} ${s.modules.trim()} — ${s.subject}`)
  if ((detail.unread ?? 0) > 0) suspects.push(`${detail.unread} more could not be read`)
  if ((detail.unreconciled ?? '') !== '') suspects.push(`Not reconciled: ${detail.unreconciled}`)
  if (suspects.length === 0) suspects.push(NO_SUSPECT_TEXT)
  return { at: note.at, text: note.text, red, failedTasks, suspects }
}

export type FullCiView = { state: 'none' } | { state: 'notes'; rows: FullCiRow[] }

export function fullCiView(notes: FullCiNotes): FullCiView {
  return notes.runs.length === 0 ? { state: 'none' } : { state: 'notes', rows: notes.runs.map(fullCiRow) }
}

/** Keep unknown and reused records visible; neither proves this module ran. */
export function matchesModule(run: Pick<UnitsRun, 'asked' | 'run_modules' | 'tested_by'>, module: string): boolean {
  if (!module || run.tested_by) return true
  const scope = modulesRun(run)
  return scope.kind !== 'some' || scope.names.includes(module)
}
