import type { SystemItem } from './catalog'

/**
 * The three real states on the city (charter 10, step 3), each tiered from a
 * read by the dictionary's thresholds: what launches, where the work queues,
 * what waits for the Owner. A read that fails is unknown, never green.
 */

export type Tone = 'calm' | 'busy' | 'alert' | 'unknown'
export type Reading = { word: string; tone: Tone; lines: string[]; count?: number; launching?: boolean }

export type InFlight = { name: string; pipeline: string; started_at?: string; delivery?: boolean }
export type Fact<T> = { value: T | null; error?: string }
export type ReleaseContext = {
  repo: string
  facts: {
    stg?: Fact<{ commit: string; run: string; completed_at: string }>
    prod?: Fact<{ commit: string; run: string; completed_at: string }>
    runs_in_flight?: Fact<InFlight[]>
    window?: Fact<{ held: boolean; who: string; what: string }>
  }
}

const ago = (iso: string, now: number) => {
  const m = Math.max(0, Math.round((now - Date.parse(iso)) / 60000))
  return m < 90 ? `${m} min ago` : m < 2880 ? `${Math.round(m / 60)} h ago` : `${Math.round(m / 1440)} d ago`
}

/** A pad's state from its repository's release context. */
export function padReading(lane: SystemItem, contexts: ReadonlyMap<string, ReleaseContext | null>, repos: readonly string[], now: number): Reading {
  const repo = repos.find(r => contexts.has(r))
  const ctx = repo ? contexts.get(repo) : undefined
  const pipeline = lane.binds.slice('lane:'.length)
  if (!ctx) return { word: 'unknown', tone: 'unknown', lines: [`release context of ${repos.join(', ') || 'its repository'} not read`] }
  const runs = ctx.facts.runs_in_flight?.value
  const win = ctx.facts.window?.value
  const env = /-prod$/.test(pipeline) ? 'prod' : 'stg'
  const last = ctx.facts[env]?.value
  const lines = [last ? `last ${env.toUpperCase()} release ${ago(last.completed_at, now)}, ${last.commit.slice(0, 8)}` : `last ${env.toUpperCase()} release not read`]
  if (runs == null) return { word: 'unknown', tone: 'unknown', lines: ['runs in flight not read', ...lines] }
  const mine = runs.filter(r => r.pipeline === pipeline)
  if (mine.length) return { word: 'launching', tone: 'busy', launching: true, lines: [`run ${mine[0].name}${mine[0].started_at ? `, started ${ago(mine[0].started_at, now)}` : ''}`, ...lines] }
  if (win?.held && repo && win.what.split(',').includes(repo)) return { word: 'window held', tone: 'busy', lines: [`window held by ${win.who}`, ...lines] }
  return { word: 'quiet', tone: 'calm', lines }
}

/** CI and landing runs in flight, from every context read; unknown when one is missing. */
export function ciReading(contexts: ReadonlyMap<string, ReleaseContext | null>): Reading {
  const runs = new Map<string, InFlight>()
  for (const [repo, ctx] of contexts) {
    const list = ctx?.facts.runs_in_flight?.value
    if (!list) return { word: 'unknown', tone: 'unknown', lines: [`runs in flight of ${repo} not read`] }
    for (const r of list) if (!r.delivery && /^bifrost-(ci-|land$)/.test(r.pipeline)) runs.set(r.name, r)
  }
  const n = runs.size
  const lines = n ? [...runs.values()].slice(0, 6).map(r => `${r.pipeline}: ${r.name}`) : ['no CI run in flight']
  if (n === 0) return { word: 'clear', tone: 'calm', lines, count: 0 }
  return { word: n >= 4 ? 'jammed' : 'busy', tone: n >= 4 ? 'alert' : 'busy', lines: [`${n} in flight; waiting or running is not read`, ...lines], count: n }
}

/** What waits for the Owner: pending requests and open questions. */
export function waitingReading(approvals: number | null, questions: number | null): Reading {
  if (approvals == null || questions == null) return { word: 'unknown', tone: 'unknown', lines: [approvals == null ? 'requests not read' : `${approvals} requests`, questions == null ? 'questions not read' : `${questions} questions`] }
  const n = approvals + questions
  const lines = [`${approvals} requests, ${questions} questions`]
  if (n === 0) return { word: 'none waiting', tone: 'calm', lines, count: 0 }
  return { word: `${n} waiting`, tone: n >= 4 ? 'alert' : 'busy', lines, count: n }
}
