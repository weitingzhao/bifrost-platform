import type { ObservationPanel, ReadEvidence } from '@bifrost/ui/city/observation'

/**
 * Signals' projection of two other lines' public reads (2026-11-23, UI / Signals):
 * - the bus's state read GET /api/v1/bus/states (communication line): cluster.workload.changed v2
 *   (ready, version) and cluster.database.changed v2 (ready, primary, backup, archiving, last_backup);
 * - the evaluation readout GET /api/v1/readings/evaluation (metrics line): metric_coverage_by_namespace.
 * Nothing is judged here: states are shown as the bus carries them, the coverage as the readout gives it,
 * and a read that failed stays unknown with its reason. A namespace is a namespace; no domain is inferred.
 */
type Get = (path: string) => Promise<unknown>
type BusState = { subject: string; state: Record<string, string>; at: string; event: string }
type BusStates = { type: string; ref?: string; since?: string; note?: string; states?: BusState[] }
type Coverage = { name: string; labels?: Record<string, string>; value: number | null; status: string; note?: string; window?: { from: string; to: string } }
type Readout = { readings?: Coverage[]; window?: { from: string; to: string } }

export const WORKLOADS = '/api/v1/bus/states?type=cluster.workload.changed'
export const DATABASES = '/api/v1/bus/states?type=cluster.database.changed'
export const COVERAGE = '/api/v1/readings/evaluation?days=1&name=metric_coverage_by_namespace'

const note = (text: string): ObservationPanel['blocks'][number] => ({ kind: 'note', text, title: text })
const BUS = 'Bus ledger read on request; each subject is as of its last change. The publisher owns the state'
const LISTED = 'Listed: subjects whose state changed within the bus retention; a subject with no change in it is not listed.'

async function read<T>(get: Get, path: string, evidence: Record<string, ReadEvidence[]>, id: string, ttl: number, measured: (value: T, receivedAt: string) => string | undefined, detail: string): Promise<T | undefined> {
  const started = new Date().toISOString()
  try {
    const value = await get(path) as T
    const receivedAt = new Date().toISOString()
    evidence[id] = [{ source: path, outcome: 'read', receivedAt, evaluatedAt: measured(value, receivedAt), expiresAt: new Date(Date.parse(receivedAt) + ttl).toISOString(), detail }]
    return value
  } catch (e) {
    evidence[id] = [{ source: path, outcome: 'error', receivedAt: started, detail: String(e) }]
    return undefined
  }
}

/** Bus states: workloads by namespace and the databases. Polled with the other operations reads. */
export async function readStates(get: Get) {
  const evidence: Record<string, ReadEvidence[]> = {}
  const [workloads, databases] = await Promise.all([
    read<BusStates>(get, WORKLOADS, evidence, '@workloads', 150000, (_, at) => at, BUS),
    read<BusStates>(get, DATABASES, evidence, '@databases', 150000, (_, at) => at, BUS),
  ])
  return { workloads, databases, evidence }
}

/** The day's closed coverage readout; it changes once a day, so it is read rarely. */
export async function readCoverage(get: Get) {
  const evidence: Record<string, ReadEvidence[]> = {}
  // Measured at the closed window's end; refreshed every 30 minutes, so two missed reads make it stale.
  const readout = await read<Readout>(get, COVERAGE, evidence, '@coverage', 3600000, v => v.window?.to, 'Evaluation readout over a closed day; measured at the window end. The metrics line owns the rule')
  return { readout, evidence }
}

export function databasePanels(databases: BusStates | undefined, evidence: Record<string, readonly ReadEvidence[]>): ObservationPanel[] {
  if (!Array.isArray(databases?.states)) {
    const why = evidence['@databases']?.[0]?.detail ?? 'Incomplete source response'
    return [{ id: '@databases', label: 'Databases', blocks: [note('Unknown · ' + why)] }]
  }
  if (!databases.states.length) return [{ id: '@databases', label: 'Databases', blocks: [note('No database changed state within the bus retention. Full list: ' + (databases.ref ?? 'the ref route'))] }]
  return databases.states.map(d => {
    const id = 'database:' + d.subject
    evidence[id] = evidence['@databases']
    const v = (k: string) => d.state[k] ?? 'Not reported'
    return { id, label: d.subject, blocks: [
      { kind: 'facts' as const, rows: [
        { label: 'Ready', value: v('ready') }, { label: 'Primary', value: v('primary') || 'None' },
        { label: 'Backup', value: v('backup') }, { label: 'WAL archiving', value: v('archiving') },
        { label: 'Last backup', value: v('last_backup') },
      ] },
      note('Bus state as of its last change ' + d.at + ' (' + d.event + '); not a fresh probe.'),
    ] }
  })
}

const pct = (v: number) => Math.round(v * 1000) / 10 + '%'

/** One panel per namespace: the workloads the bus lists and that namespace's coverage reading. */
export function workloadPanels(workloads: BusStates | undefined, readout: Readout | undefined, evidence: Record<string, readonly ReadEvidence[]>): ObservationPanel[] {
  const byNs = new Map<string, BusState[]>()
  for (const s of workloads?.states ?? []) {
    const ns = s.subject.split('/')[0]
    byNs.set(ns, [...(byNs.get(ns) ?? []), s])
  }
  const coverage = new Map<string, Coverage>()
  for (const r of readout?.readings ?? []) if (r.name === 'metric_coverage_by_namespace' && r.labels?.namespace) coverage.set(r.labels.namespace, r)
  const statesWhy = Array.isArray(workloads?.states) ? undefined : evidence['@workloads']?.[0]?.detail ?? 'Incomplete source response'
  const coverageWhy = readout ? undefined : evidence['@coverage']?.[0]?.detail ?? 'Not read yet'
  const window = readout?.window ? readout.window.from + ' → ' + readout.window.to : undefined
  const names = [...new Set([...byNs.keys(), ...coverage.keys()])].sort()
  if (!names.length) return [{ id: '@workloads', label: 'Workloads', blocks: [note('Unknown · ' + (statesWhy ?? coverageWhy ?? 'No namespaces returned'))] }]
  return names.map(ns => {
    const id = 'workloads:' + ns
    evidence[id] = [...(evidence['@workloads'] ?? []), ...(evidence['@coverage'] ?? [])]
    const listed = byNs.get(ns) ?? []
    const count = (v: string) => listed.filter(s => s.state.ready === v).length
    const c = coverage.get(ns)
    const coverageValue = coverageWhy ? 'Unknown' : !c ? 'Not in readout' : c.status === 'ok' && c.value !== null ? pct(c.value) : c.status
    const rows = [
      { label: 'Listed on bus', value: statesWhy ? 'Unknown' : listed.length },
      { label: 'Ready', value: statesWhy ? 'Unknown' : count('ready') },
      { label: 'Not ready', value: statesWhy ? 'Unknown' : count('notready'), tone: count('notready') ? 'warn' as const : undefined },
      { label: 'Own metrics', value: coverageValue },
    ].map(r => ({ label: r.label, value: r.tone ? { text: String(r.value), tone: r.tone } : r.value }))
    return { id, label: ns, blocks: [
      { kind: 'facts' as const, rows },
      ...(c?.note ? [note(c.note)] : []),
      ...(statesWhy ? [note('Workload states unknown · ' + statesWhy)] : listed.length ? [] : [note('No workload of this namespace changed state within the bus retention.')]),
      ...(coverageWhy ? [note('Coverage unknown · ' + coverageWhy)] : [note('Coverage: metrics line readout, closed day ' + (window ?? 'window not given') + '; a down target still counts as covered.')]),
      // One compact sign on the card; the evidence drawer shows the whole list.
      ...(listed.length ? [note('Workloads: ' + listed.map(s => s.subject.slice(ns.length + 1) + ' · ' + (s.state.ready ?? 'not reported') + ' · ' + (s.state.version || 'no version') + ' · changed ' + s.at).join(' | ') + '. ' + LISTED)] : []),
    ] }
  })
}
