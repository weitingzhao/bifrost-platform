import { authedFetch } from '@/api/client'

/**
 * GET /api/v1/bus/nextgen and /api/v1/bus/contract: the communication
 * layer's change of generation and its contract, both read from the
 * registers compiled into the platform (bus/nextgen, bus/registry), whose
 * tests hold them to the code. Nothing here is typed in on the page.
 */

export type NextgenChannel = {
  id: string
  class: string
  disposition: string
  what: string
  by: string
  retired?: { at: string; change: string; outside?: string }
}
export type NextgenCount = { disposition: string; total: number; retired: number }
/** Whether a moved reader really runs on its new source in this environment (absent from an older platform). */
export type NextgenEvidence = { state: 'running' | 'stale' | 'none' | 'unmeasured'; last_seen?: string; runs_7d: number; stale_after_seconds?: number }
export type NextgenReader = { id: string; what: string; status: string; at?: string; owner?: string; note?: string; evidence?: NextgenEvidence }
export type NextgenReaderGroup = { id: string; title: string; readers: NextgenReader[]; counts: Record<string, number> }
/** One step of the switch: its channels, those cut over, those proven running here, the old roads gone. */
export type NextgenStep = { step: string; label: string; total: number; cut: number; proven: number; unproven?: string[]; unmeasured: number; retired: number }
export type NextgenBaseline = { key: string; label: string; at: string; note?: string; value?: number; unmeasured?: string }
/** The channels that report problems: connected of live, and what is left. Absent from an older platform. */
export type NextgenProblems = {
  live: number; connected: number; proven: number; unmeasured: number; deleted: number; pending: number; todo: number
  unproven: string[]; open: string[]; done: boolean
  /** Channels with no real run whose drill ran (bus/drill), and which. Absent from an older platform. */
  drilled?: number; drilled_only?: string[]
}

/** How many channels a real run proved and how many only a drill did. */
export function problemProofText(p: Pick<NextgenProblems, 'proven' | 'drilled'>): string {
  return `${p.proven} proven by a real run · ${p.drilled ?? 0} by a drill only`
}
export type BusNextgen = {
  generated_at: string
  problems?: NextgenProblems
  progress: {
    start: number; now: number; target: number; retired: number; to_go: number; counts: NextgenCount[]; recent: NextgenChannel[]
    /** Absent from an older platform. */
    removable?: number; removed?: number; stays?: number; new_pieces?: number; end?: number; steps?: NextgenStep[]
  }
  readers: NextgenReaderGroup[]
  baselines: NextgenBaseline[]
}

/** A reader called moved with no recent proof that it runs: not finished, whatever the register says. */
export function movedWithoutEvidence(r: NextgenReader): boolean {
  return r.status === 'moved' && (r.evidence?.state === 'stale' || r.evidence?.state === 'none')
}

export async function fetchBusNextgen(): Promise<BusNextgen> {
  const r = await authedFetch('bus nextgen', '/api/v1/bus/nextgen')
  return (await r.json()) as BusNextgen
}

export type ContractTopic = { tenant: string; type: string; version: number; publisher: string; domain?: string; guarantee: string; consumers: string[]; exports?: string[] }
/** domain: the domain a row's delivery reaches (a wake: the seats', coordination; a notify: the owner's), from the registry. */
export type ContractRow = { tenant: string; id: string; on: string[]; deliver: string; to?: string; action?: string; by: string; envs?: string[]; domain?: string }
/** domain: the domain whose code runs the client (a durable reader), as its register declares. */
export type ContractClient = { tenant: string; name: string; kind: string; lost_after?: string; envs?: string[]; domain?: string }
export type BusContract = { topics: ContractTopic[]; subscriptions: ContractRow[]; clients: ContractClient[] }

export async function fetchBusContract(): Promise<BusContract> {
  const r = await authedFetch('bus contract', '/api/v1/bus/contract')
  const b = (await r.json()) as BusContract
  return { topics: b.topics ?? [], subscriptions: b.subscriptions ?? [], clients: b.clients ?? [] }
}

/** The bar's segments: each disposition's share of the start, its retired part first. */
export function generationSegments(counts: readonly NextgenCount[], start: number): { disposition: string; retired: number; left: number }[] {
  if (start <= 0) return []
  return counts.filter(c => c.total > 0).map(c => ({
    disposition: c.disposition,
    retired: (c.retired / start) * 100,
    left: ((c.total - c.retired) / start) * 100,
  }))
}

/** A subscriber as the graph names it: a thread's session shortened, anything else as it is. */
export function shortAddress(a: string): string {
  const m = /^thread:(local_[0-9a-f]{8})/.exec(a)
  return m ? `thread:${m[1].slice(6)}…` : a
}

export type ContractGraphModel = {
  publishers: string[]
  topics: string[]
  subscribers: string[]
  edges: { from: string; to: string; kind: 'publishes' | 'reads' | 'wakes'; label?: string }[]
}

/** Publisher → topic → subscriber, from the contract: declared readers and the subscription rows' wakes. */
export function contractGraph(c: BusContract): ContractGraphModel {
  const pubs = new Set<string>()
  const subs = new Set<string>()
  const edges: ContractGraphModel['edges'] = []
  const topics = c.topics.map(t => t.type)
  for (const t of c.topics) {
    const pub = t.publisher || 'unknown'
    pubs.add(pub)
    edges.push({ from: `p:${pub}`, to: `t:${t.type}`, kind: 'publishes' })
    for (const r of t.consumers ?? []) {
      subs.add(r)
      edges.push({ from: `t:${t.type}`, to: `s:${r}`, kind: 'reads' })
    }
  }
  for (const row of c.subscriptions) {
    const target = row.deliver === 'wake' ? row.to ?? '' : row.action ?? row.deliver
    if (!target) continue
    const name = shortAddress(target)
    subs.add(name)
    for (const on of row.on) {
      if (topics.includes(on)) edges.push({ from: `t:${on}`, to: `s:${name}`, kind: 'wakes', label: row.id })
    }
  }
  return { publishers: [...pubs].sort(), topics: [...topics].sort(), subscribers: [...subs].sort(), edges }
}
