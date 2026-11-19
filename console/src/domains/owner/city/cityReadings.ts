import { placeOf, satelliteOf, type CityLane } from './catalog'

/**
 * The real states on the city (charter 10, step 3), each tiered from a read by
 * the dictionary's thresholds. Two lines (the Owner, 2026-11-18): the rocket is
 * the platform, built in the horizontal hangar (CI), stacked in the vertical
 * assembly building (STG) and launched from the pad (PROD), one vehicle flown
 * again and again; the satellites are the tenants, built and tested in their
 * hangar and carried up by whichever rocket is current. A read that fails is
 * unknown, never green.
 */

export type Tone = 'calm' | 'busy' | 'alert' | 'unknown'
export type Reading = { word: string; tone: Tone; lines: string[]; count?: number; launching?: boolean; active?: boolean; payload?: { name: string; color: string } }

export type InFlight = { name: string; pipeline: string; started_at?: string; delivery?: boolean }
export type Fact<T> = { value: T | null; error?: string }
export type Release = { commit: string; run: string; completed_at: string }
export type ReleaseContext = {
  repo: string
  facts: {
    stg?: Fact<Release>
    prod?: Fact<Release>
    runs_in_flight?: Fact<InFlight[]>
    window?: Fact<{ held: boolean; who: string; what: string }>
  }
}
/** Each repository's release context; null where it could not be read. */
export type Contexts = ReadonlyMap<string, ReleaseContext | null>

export type Satellite = { name: string; color: string; word: string; tone: Tone; launching: boolean; lines: string[] }
export type Flow = { rocketStg: boolean; rocketProd: boolean; satelliteProd: boolean }
export type Sky = { satellites: Satellite[]; flow: Flow }

const ago = (iso: string, now: number) => {
  const m = Math.max(0, Math.round((now - Date.parse(iso)) / 60000))
  return m < 90 ? `${m} min ago` : m < 2880 ? `${Math.round(m / 60)} h ago` : `${Math.round(m / 1440)} d ago`
}

/** Every run in flight that a read context reports, with the repositories that were not read. */
function inFlight(contexts: Contexts, lanes: readonly CityLane[]) {
  const runs = new Map<string, InFlight>(), unread: string[] = []
  for (const [repo, ctx] of contexts) {
    const list = ctx?.facts.runs_in_flight?.value
    if (!list) { unread.push(repo); continue }
    for (const r of list) runs.set(r.name, r)
  }
  const laneOf = new Map(lanes.map(l => [l.pipeline, l]))
  const at = (r: InFlight) => {
    const l = laneOf.get(r.pipeline) ?? { pipeline: r.pipeline, repos: [] }
    return placeOf(l)
  }
  return { runs: [...runs.values()], at, unread }
}

const unknown = (why: string[]): Reading => ({ word: 'unknown', tone: 'unknown', lines: why })

/** The horizontal hangar: the rocket's CI and landing runs, as crates at its door. */
export function rocketCiReading(contexts: Contexts, lanes: readonly CityLane[]): Reading {
  if (!contexts.get('bifrost-platform')) return unknown(['the platform\'s release context was not read'])
  const { runs, at } = inFlight(contexts, lanes)
  const mine = runs.filter(r => { const p = at(r); return p.line === 'rocket' && p.step === 'ci' })
  return crates(mine, 'in CI')
}

function crates(runs: InFlight[], what: string): Reading {
  const n = runs.length, lines = n ? runs.slice(0, 6).map(r => `${r.pipeline}: ${r.name}`) : [`nothing ${what}`]
  if (n === 0) return { word: 'clear', tone: 'calm', lines, count: 0 }
  return { word: `${n} ${what}`, tone: n >= 4 ? 'alert' : 'busy', lines: [`${n} ${what}; waiting or running is not read`, ...lines], count: n }
}

/** The vertical assembly building: the rocket being stacked for STG. */
export function rocketStgReading(contexts: Contexts, lanes: readonly CityLane[], now: number): Reading {
  const ctx = contexts.get('bifrost-platform')
  if (!ctx) return unknown(['the platform\'s release context was not read'])
  const { runs, at } = inFlight(contexts, lanes)
  const last = ctx.facts.stg?.value
  const tail = [last ? `STG runs ${last.commit.slice(0, 8)}, released ${ago(last.completed_at, now)}` : 'what STG runs was not read']
  const mine = runs.filter(r => { const p = at(r); return p.line === 'rocket' && p.step === 'stg' })
  if (mine.length) return { word: 'stacking', tone: 'busy', active: true, lines: [`run ${mine[0].name}`, ...tail] }
  return { word: 'quiet', tone: 'calm', lines: tail }
}

/** The satellite hangar: the tenants' CI runs as crates, their STG runs as a lit test bay. */
export function satellitesReading(contexts: Contexts, lanes: readonly CityLane[]): Reading {
  const { runs, at, unread } = inFlight(contexts, lanes)
  const ci = runs.filter(r => { const p = at(r); return p.line === 'satellite' && p.step === 'ci' })
  const stg = runs.filter(r => { const p = at(r); return p.line === 'satellite' && (p.step === 'stg' || p.step === 'build') })
  const lines = [...stg.map(r => `testing: ${r.pipeline}`), ...ci.slice(0, 5).map(r => `CI: ${r.pipeline}`)]
  if (unread.length) return { ...unknown([`release context not read: ${unread.join(', ')}`, ...lines]), count: ci.length }
  if (stg.length) return { word: 'testing', tone: 'busy', active: true, count: ci.length, lines }
  if (ci.length) return { ...crates(ci, 'in CI'), active: false }
  return { word: 'quiet', tone: 'calm', count: 0, lines: ['nothing in CI or testing'] }
}

/** The pad: the rocket launching on its own (an upgrade) or carrying a satellite, and which rocket that is. */
export function padReading(contexts: Contexts, lanes: readonly CityLane[], now: number): Reading {
  const ctx = contexts.get('bifrost-platform')
  if (!ctx) return unknown(['the platform\'s release context was not read'])
  const { runs, at, unread } = inFlight(contexts, lanes)
  const rocket = ctx.facts.prod?.value
  const tail = [rocket ? `the rocket in service: ${rocket.commit.slice(0, 8)}, flown ${ago(rocket.completed_at, now)}` : 'the rocket in service was not read']
  const upgrade = runs.find(r => { const p = at(r); return p.line === 'rocket' && p.step === 'prod' })
  if (upgrade) return { word: 'launching the new rocket', tone: 'busy', launching: true, lines: [`run ${upgrade.name}`, ...tail] }
  const carried = runs.find(r => { const p = at(r); return p.line === 'satellite' && p.step === 'prod' })
  if (carried) {
    const lane = lanes.find(l => l.pipeline === carried.pipeline) ?? { pipeline: carried.pipeline, repos: [] }
    const sat = satelliteOf(lane)
    return { word: `launching ${sat.name}`, tone: 'busy', launching: true, payload: sat, lines: [`run ${carried.name}`, `${sat.name} rides the rocket in service`, ...tail] }
  }
  const win = ctx.facts.window?.value
  if (win?.held && win.what.split(',').includes('bifrost-platform')) return { word: 'window held', tone: 'busy', lines: [`window held by ${win.who}`, ...tail] }
  if (unread.length) return { word: 'quiet', tone: 'unknown', lines: [`release context not read: ${unread.join(', ')}`, ...tail] }
  return { word: 'quiet', tone: 'calm', lines: tail }
}

/** The satellites in orbit: one per tenant that launches to PROD, with the version it flies, or launching now. */
export function skyReading(contexts: Contexts, lanes: readonly CityLane[], now: number): Sky {
  const { runs, at } = inFlight(contexts, lanes)
  const prod = lanes.filter(l => { const p = placeOf(l); return p.line === 'satellite' && (p.step === 'prod' || p.step === 'build') })
  const byName = new Map<string, Satellite>()
  for (const l of prod) {
    // One satellite a tenant: the lanes of one repository (research and its Dagster image) are one satellite.
    const own = satelliteOf(l)
    const sat = own.name === 'Research dagster' ? { ...own, name: 'Research' } : own
    const going = runs.find(r => r.pipeline === l.pipeline && at(r).step !== 'ci')
    const repo = l.repos.find(r => contexts.has(r))
    const fact = repo ? contexts.get(repo)?.facts.prod : undefined
    const lines = [fact?.value ? `in orbit: ${fact.value.commit.slice(0, 8)}, launched ${ago(fact.value.completed_at, now)}` : 'the version in orbit is not read']
    const prev = byName.get(sat.name)
    const s: Satellite = going
      ? { ...sat, word: 'launching', tone: 'busy', launching: true, lines: [`run ${going.name}`, ...lines] }
      : { ...sat, word: fact?.value ? fact.value.commit.slice(0, 8) : 'unknown', tone: fact?.value ? 'calm' : 'unknown', launching: false, lines }
    if (!prev || s.launching || (prev.tone === 'unknown' && s.tone !== 'unknown')) byName.set(sat.name, s)
  }
  const step = (line: 'rocket' | 'satellite', st: string) => runs.some(r => { const p = at(r); return p.line === line && p.step === st })
  return { satellites: [...byName.values()], flow: { rocketStg: step('rocket', 'stg'), rocketProd: step('rocket', 'prod'), satelliteProd: step('satellite', 'prod') } }
}

/** What waits for the Owner: pending requests and open questions. */
export function waitingReading(approvals: number | null, questions: number | null): Reading {
  if (approvals == null || questions == null) return { word: 'unknown', tone: 'unknown', lines: [approvals == null ? 'requests not read' : `${approvals} requests`, questions == null ? 'questions not read' : `${questions} questions`] }
  const n = approvals + questions
  const lines = [`${approvals} requests, ${questions} questions`]
  if (n === 0) return { word: 'none waiting', tone: 'calm', lines, count: 0 }
  return { word: `${n} waiting`, tone: n >= 4 ? 'alert' : 'busy', lines, count: n }
}
