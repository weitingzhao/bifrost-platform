import { objectsIn, pathKey, type ControlRoomModel, type ObservationSnapshot, type ReadEvidence, type SignalItem, type SignalTrend } from '@bifrost/ui/city/observation'
import { statusOfState, statusOfWord, type StatusWord } from '@bifrost/ui/city/world'
import type { DomainStatusResponse } from '@/domains/foundation/catalog/api/domainStatus'
import type { DeliveryPipelineRunsResponse } from '@/domains/delivery/api/deliveryTypes'
import type { DataHusbandrySnapshot } from '@/domains/tenant/husbandry/api/dataHusbandry'
import { NO_ADDRESS } from './read'
import type { DomainEdge } from './controlRoomReads'

/**
 * Signals' control room (Owner 2026-11-24: a power plant's big screen, not a wall of cards; 箭10 approved the
 * prototype and its corrections). Every signal the page already reads becomes one item with the source's word as one
 * of the seven and one plain sentence; the source's own text goes to `source`, its reads to `evidence`. Nothing is
 * judged beyond that mapping. Two cases are "not read by design" (boundary, quiet), decided here and nowhere else:
 * - a device the topology gives no readable address (read.ts NO_ADDRESS);
 * - a domain light whose source says its contract names no interface.
 */
type Raw = { domains?: DomainStatusResponse; ci: { name: string; data?: DeliveryPipelineRunsResponse }[]; ciCatalog?: boolean; husbandry?: DataHusbandrySnapshot }
type BusState = { subject: string; state: Record<string, string | undefined>; at?: string }
export type ControlRoomInput = {
  hardware: ObservationSnapshot
  operations?: { raw: Raw; evidence: Record<string, readonly ReadEvidence[]> }
  databases?: { states?: BusState[] }
  workloads?: { states?: BusState[] }
  busEvidence?: Record<string, readonly ReadEvidence[]>
  edges?: DomainEdge[]
  edgesEvidence?: ReadEvidence
  trends?: SignalTrend[]
}
// boundary ranks just above ok: a thing quiet by design is still said as such, never folded into ok
const RANK: Record<StatusWord, number> = { fail: 5, warn: 4, unknown: 3, pending: 2, info: 1, boundary: 0.5, ok: 0 }
const worst = (ws: StatusWord[]) => ws.reduce<StatusWord>((a, b) => (RANK[b] > RANK[a] ? b : a), 'ok')
const BY_DESIGN = /contract names no interface/i

const LIGHTS = [['alive', 'Services'], ['interfaces', 'Interfaces'], ['boundary', 'Boundary'], ['fresh', 'Freshness']] as const
/** A domain light's plain sentence: the known source phrasings, else the light and the source's word. */
function plainLight(label: string, value: string | undefined, why: string | undefined): string {
  const w = why ?? ''
  if (/no running workload/i.test(w)) return 'No running workload of it is seen'
  if (/too few samples/i.test(w)) return 'Too little traffic to judge its interfaces'
  if (BY_DESIGN.test(w)) return 'Its contract names no interface (not measured by design)'
  if (/import against the tiers/i.test(w) && value !== 'good') return 'It imports against the tier order'
  return `${label} reported ${value ?? 'nothing'}`
}

export function controlRoomOf(input: ControlRoomInput): ControlRoomModel {
  const items: SignalItem[] = []
  const ev = input.operations?.evidence ?? {}

  // Domains: the catalog's four lights per domain.
  const domains = input.operations?.raw.domains?.domains
  const edges = input.edges ?? []
  if (domains) for (const d of domains) {
    const lights = LIGHTS.map(([k, label]) => {
      const l = d[k] as { value?: string; why?: string } | undefined
      const word: StatusWord = l?.value === 'unknown' && BY_DESIGN.test(l?.why ?? '') ? 'boundary' : statusOfWord(l?.value)
      return { label, word, value: l?.value, why: l?.why }
    })
    const off = lights.filter(l => l.word !== 'ok').sort((a, b) => RANK[b.word] - RANK[a.word])
    items.push({ id: 'domain:' + d.id, stage: 'domains', name: d.id, word: worst(lights.map(l => l.word)),
      plain: off.length ? plainLight(off[0].label, off[0].value, off[0].why) : 'All four lights good',
      source: off.length ? off.map(l => `${l.label} ${l.value}: ${l.why}`).join(' · ') : undefined,
      lamps: lights.map(l => ({ label: l.label.toLowerCase(), word: l.word, detail: `${l.value}: ${l.why}` })),
      uses: edges.filter(e => e.from === d.id).map(e => 'domain:' + e.to), usedBy: edges.filter(e => e.to === d.id).map(e => 'domain:' + e.from),
      evidence: [...(ev['domain:' + d.id] ?? ev['@domains'] ?? []), ...(input.edgesEvidence ? [input.edgesEvidence] : [])] })
  } else if (!input.operations) items.push({ id: '@domains', stage: 'domains', name: 'Domain status', word: 'pending', plain: 'Reading…', evidence: [] })
  else items.push({ id: '@domains', stage: 'domains', name: 'Domain status', word: 'unknown', plain: 'The domain status could not be read', source: ev['@domains']?.[0]?.detail, evidence: ev['@domains'] ?? [] })

  // CI: each registered CI pipeline's latest retained run.
  if (input.operations && input.operations.raw.ciCatalog === false) items.push({ id: '@ci', stage: 'ci', name: 'CI catalog', word: 'unknown', plain: 'The CI pipeline list could not be read', source: ev['@ci']?.[0]?.detail, evidence: ev['@ci'] ?? [] })
  for (const { name, data } of input.operations?.raw.ci ?? []) {
    const key = 'ci:' + name, label = name.replace('bifrost-ci-', '')
    if (!Array.isArray(data?.runs)) { items.push({ id: key, stage: 'ci', name: label, word: 'unknown', plain: 'Its runs could not be read', source: ev[key]?.[0]?.detail, evidence: ev[key] ?? [] }); continue }
    const runs = [...data.runs].sort((a, b) => (b.start_time ?? '').localeCompare(a.start_time ?? '')), latest = runs[0]
    const end = Date.parse(data.generated_at), day = runs.filter(r => r.start_time && Date.parse(r.start_time) > end - 864e5)
    const failed = day.filter(r => r.status === 'False').length
    const word: StatusWord = data.reachability === 'fail' ? 'unknown' : !latest ? 'ok' : latest.status === 'False' ? 'fail' : latest.status === 'Unknown' ? 'pending' : statusOfWord(latest.status === 'True' ? 'ok' : latest.status)
    items.push({ id: key, stage: 'ci', name: label, word,
      plain: !latest ? 'No retained runs' : word === 'fail' ? 'Its latest run failed' : word === 'pending' ? 'A run is in progress' : 'Latest run passed',
      lamps: [{ label: 'latest run', word, detail: latest ? `${latest.name}: ${latest.reason ?? latest.status}` : 'no retained runs' }, { label: 'reach', word: statusOfWord(data.reachability), detail: data.reachability }],
      source: latest ? `latest ${latest.name}: ${latest.status}${latest.reason ? ' (' + latest.reason + ')' : ''} · ${day.length} runs in 24h, ${failed} failed · reachability ${data.reachability}` : `reachability ${data.reachability}`,
      evidence: ev[key] ?? [] })
  }

  // Data: the husbandry lanes' verdicts and each database's own state.
  const hb = input.operations?.raw.husbandry
  if (hb?.lanes) for (const l of hb.lanes) {
    const word = statusOfWord(l.verdict)
    items.push({ id: 'data:' + l.id, stage: 'data', name: l.label, word, lamps: [{ label: 'reported', word, detail: `${l.verdict}: ${l.detail}` }], plain: word === 'ok' ? 'Healthy' : `Reported ${l.verdict}`, source: `${l.verdict}: ${l.detail}`, evidence: ev['data:' + l.id] ?? [] })
  } else if (input.operations) items.push({ id: '@data', stage: 'data', name: 'Data husbandry', word: 'unknown', plain: 'The data lanes could not be read', source: ev['@data']?.[0]?.detail, evidence: ev['@data'] ?? [] })
  for (const db of input.databases?.states ?? []) {
    const s = db.state, parts = (['ready', 'backup', 'archiving'] as const).map(k => ({ k, w: statusOfWord(s[k]) }))
    const bad = parts.filter(p => p.w !== 'ok').sort((a, b) => RANK[b.w] - RANK[a.w])[0]
    items.push({ id: 'db:' + db.subject, stage: 'data', name: db.subject.split('/').at(-1) ?? db.subject, word: worst(parts.map(p => p.w)),
      lamps: parts.map(p => ({ label: p.k, word: p.w, detail: s[p.k] })),
      plain: bad ? `${bad.k[0].toUpperCase() + bad.k.slice(1)} reported ${s[bad.k] ?? 'nothing'}` : 'ready, backup and archiving all report ok',
      source: `ready ${s.ready} · primary ${s.primary} · backup ${s.backup} · archiving ${s.archiving} · last backup ${s.last_backup}`, evidence: input.busEvidence?.['@databases'] ?? [] })
  }

  // Devices: the hardware snapshot's buildings and the alerts placed on them; alerts placed nowhere stand alone.
  const snap = input.hardware, alerts = snap.content.alerts?.alerts ?? [], alertEv = snap.evidence['@alerts'] ?? []
  const sev = (s: string): StatusWord => s === 'critical' ? 'fail' : s === 'warning' ? 'warn' : 'info'
  for (const { place, path } of objectsIn(snap.content.tree).filter(o => o.place.kind === 'building')) {
    const reads = snap.evidence[pathKey(path)] ?? []
    const byDesign = place.state === 'unread' && reads.some(r => r.detail === NO_ADDRESS)
    const own = alerts.filter(a => a.at?.[1] === place.id && sev(a.severity) !== 'info')
    const stateWord: StatusWord = byDesign ? 'boundary' : statusOfState(place.state)
    const word = worst([stateWord, ...own.map(a => sev(a.severity))])
    const meter = (id: string) => { const m = place.children.find(c => c.id === id)?.meter; return m && m.used != null && m.of ? m.used / m.of : null }
    items.push({ id: 'device:' + place.id, stage: 'devices', name: place.id, word,
      plain: own[0]?.summary || (byDesign ? 'Not read by design: no address in the topology' : place.state === 'down' ? 'Not reporting: down' : place.state === 'unread' ? 'No readings: its exporter is not seen' : place.state === 'standby' ? 'Asleep on purpose' : 'Online'),
      source: [...own.map(a => `${a.name} (${a.severity}): ${a.summary}`), `state ${place.state ?? 'not read'}`].join(' · '),
      lamps: [{ label: 'state', word: stateWord, detail: place.state ?? 'not read' }, { label: 'alerts', word: worst(own.map(a => sev(a.severity))), detail: own.length ? own.map(a => a.name).join(', ') : 'none firing' }],
      meters: [{ label: 'CPU', share: meter('cpu') }, { label: 'Memory', share: meter('memory') }],
      evidence: own.length ? [...alertEv, ...reads] : reads })
  }
  // The alert's id carries its labels (content/alerts.ts): the pod, instance or job tells two of the same name apart.
  const subject = (id: string) => { const l = Object.fromEntries(id.split('|')[1]?.split(',').map(kv => kv.split('=')) ?? []); return l.pod ?? l.instance ?? l.job ?? l.namespace }
  for (const a of alerts.filter(a => !a.at && sev(a.severity) !== 'info')) items.push({ id: 'alert:' + a.id, stage: 'alerts', name: a.name + (subject(a.id) ? ' · ' + subject(a.id) : ''), word: sev(a.severity), lamps: [{ label: 'firing', word: sev(a.severity), detail: a.severity }],
    plain: a.summary || 'Monitoring reports it firing', source: `${a.name} (${a.severity})${a.since ? ' since ' + a.since : ''}: ${a.summary}`, evidence: alertEv })

  const workloads = input.workloads?.states
  const notReady = workloads?.filter(w => w.state.ready !== 'ready') ?? []
  for (const w of notReady) items.push({ id: 'workload:' + w.subject, stage: 'devices', name: w.subject, word: statusOfWord(w.state.ready), lamps: [{ label: 'ready', word: statusOfWord(w.state.ready), detail: w.state.ready }],
    plain: `Workload reported ${w.state.ready ?? 'nothing'}`, source: `${w.subject}: ready ${w.state.ready}`, evidence: input.busEvidence?.['@workloads'] ?? [] })
  const infoAlerts = alerts.filter(a => sev(a.severity) === 'info').length
  const notes = [
    workloads ? `${workloads.length - notReady.length} of ${workloads.length} workloads ready (bus)` : 'workloads not read',
    snap.content.alerts?.alerts ? `${infoAlerts} info alerts not counted` : `alerts not read: ${snap.content.alerts?.why ?? 'unknown'}`,
  ]
  const stages = [{ id: 'domains', label: 'Domains' }, { id: 'ci', label: 'CI' }, { id: 'data', label: 'Data' }, { id: 'devices', label: 'Devices' }]
  if (items.some(i => i.stage === 'alerts')) stages.push({ id: 'alerts', label: 'Other alerts' })
  return { stages, items, trends: input.trends ?? [], notes }
}
