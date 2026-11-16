import { authedFetch } from './client'

/*
 * CI's own views (Work > CI): what a project declares for CI, the CI runs retained for it, and the notes of the daily
 * full run. The bodies are the ones under api/internal/server/testdata/contract/units; the fields absent from some
 * objects there are the optional ones here.
 */

export type UnitsPin = { repo: string; commit: string; tree?: string }
export type UnitsTaskCount = { task: string; units: number; shards?: number }
export type UnitsModule = {
  name: string
  kind?: string
  root: string
  image?: string
  template?: string
  /** Inputs of every task beside the module's own tree; "other:path" is a path of a pinned repository. */
  needs: string[]
  tasks: UnitsTaskCount[]
  units: number
  /** Units that declare inputs of their own. */
  declared_inputs: number
  /** Units whose declared input is the whole repository: they run on every change. */
  whole_repository: number
}
export type DomainPackage = {
  path: string; module: string; domains: string[]; status: string; basis: string
  imports: string[]; test_imports: string[]; tasks: string[]; inputs: string[]
}
export type FrontendDomainGroup = {
 path: string; files: string[]; domains: string[]; status: string; basis: string
 imports: string[]; test_imports: string[]; source_inputs: string[]; test_inputs: string[]; external_imports: string[]; unresolved_imports: string[]
}
export type FrontendDomainMap = {
 root: string; module: string; source: string; groups: FrontendDomainGroup[]
 tasks: string[]; inputs: string[]; files: number; unresolved: number; inventory_complete: boolean
}
export type DomainMap = {
 frontend?: FrontendDomainMap; frontend_note?: string
  source: string; domains: { id: string; name: string; source: string }[]
  shared_tasks: { unit: string; task: string; module: string }[]
  packages: DomainPackage[]; inventory_complete: boolean; unresolved: number
}
export type UnitsConfig = {
  domains?: DomainMap
  domain_note?: string
  repo: string
  commit: string
  declared: boolean
  note?: string
  pins: UnitsPin[]
  modules: UnitsModule[]
}

export type UnitsRunTask = { task: string; status: string; seconds?: number; waited?: number }
export type UnitsRun = {
  name: string
  pipeline: string
  revision: string
  /** Succeeded | Failed | Running | Unknown. */
  status: string
  trigger: string
  full: boolean
  created_at: string
  seconds?: number
  tested_by?: string
  /** false: the run's pipeline does not report which modules ran. */
  asked: boolean
  /** "*" or module names each between spaces. */
  run_modules?: string
  scope_why?: string
  tasks: UnitsRunTask[]
}
export type UnitsRuns = { repo: string; as_of: string; runs: UnitsRun[] }

export type FullCiSuspect = { commit: string; subject: string; run: string; modules: string }
export type FullCiDetail = {
  started?: string[]
  alert?: boolean
  run?: string
  commit?: string
  failed_tasks?: string[]
  failed_key?: string
  suspects?: FullCiSuspect[]
  unread?: number
  unreconciled?: string
}
export type FullCiNote = { at: string; text: string; full_ci: FullCiDetail }
export type FullCiNotes = { runs: FullCiNote[] }

const record = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)

/** What the project declares for CI at its main's head. */
export async function fetchUnitsConfig(repo: string, signal?: AbortSignal): Promise<UnitsConfig> {
  const value: unknown = await (await authedFetch('CI configuration', `/api/v1/units/config?repo=${encodeURIComponent(repo)}`, { signal })).json()
  if (!record(value) || typeof value.declared !== 'boolean' || !Array.isArray(value.modules) || !Array.isArray(value.pins)
    || !value.modules.every(m => record(m) && typeof m.name === 'string' && Array.isArray(m.needs) && Array.isArray(m.tasks))) {
    throw new Error('CI configuration: incomplete answer; what the project declares is not known')
  }
  return value as unknown as UnitsConfig
}

/** The CI runs retained for the project, newest first. */
export async function fetchUnitsRuns(repo: string, signal?: AbortSignal): Promise<UnitsRuns> {
  const value: unknown = await (await authedFetch('CI runs', `/api/v1/units/runs?repo=${encodeURIComponent(repo)}`, { signal })).json()
  if (!record(value) || !Array.isArray(value.runs)
    || !value.runs.every(r => record(r) && typeof r.name === 'string' && typeof r.asked === 'boolean' && Array.isArray(r.tasks))) {
    throw new Error('CI runs: incomplete answer; which runs are retained is not known')
  }
  return value as unknown as UnitsRuns
}

/** The notes of the daily full run, newest first. */
export async function fetchFullCi(signal?: AbortSignal): Promise<FullCiNotes> {
  const value: unknown = await (await authedFetch('Daily full run', '/api/v1/units/full-ci', { signal })).json()
  if (!record(value) || !Array.isArray(value.runs) || !value.runs.every(r => record(r) && typeof r.text === 'string')) {
    throw new Error('Daily full run: incomplete answer; what the full run found is not known')
  }
  return value as unknown as FullCiNotes
}

/** A declaration/evidence view at an exact commit, not a historical execution trace. */
export type UnitsPlan = {
  repo: string; commit: string
  units: { unit: string; task: string; module: string; input_key: string; verdict: string; inputs?: Record<string, unknown> }[]
}
export async function fetchUnitsPlan(repo: string, commit: string, signal?: AbortSignal): Promise<UnitsPlan> {
  const value: unknown = await (await authedFetch('CI unit inputs', `/api/v1/units/plan?${new URLSearchParams({ repo, commit, inputs: '1' })}`, { signal })).json()
  if (!record(value) || !Array.isArray(value.units) || !value.units.every(u => record(u) && typeof u.unit === 'string' && typeof u.module === 'string' && typeof u.task === 'string')) {
    throw new Error('CI unit inputs: incomplete answer')
  }
  return value as UnitsPlan
}
