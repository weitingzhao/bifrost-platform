import { useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import CityGen2 from '@bifrost/ui/city/CityGen2'
import type { CiDomainGraph } from '../src/api/ciDomains'
import { GRAPH_REPO, type GraphRead } from '../src/domains/owner/city/gen2/content/techTree'
import GRAPH_SNAPSHOT from './domain-graph.snapshot.json'
import { consoleContent } from '../src/domains/owner/city/gen2/content/console'
import { formatPath, parsePath, type Path, type Place } from '@bifrost/ui/city/places'
import { hardwareDistrict, readHardware } from '../src/domains/owner/city/gen2/content/hardware'
import { readUnits } from '../src/domains/owner/city/gen2/content/unitRooms'
import { type DomainsRead } from '../src/domains/owner/city/gen2/content/domainsPanel'
import type { DomainStatusResponse } from '../src/domains/foundation/catalog/api/domainStatus'
import OWNERSHIP from '../../config/domain-ownership.json'

const NO_LIGHT = { value: '', why: 'bench: not read' }
import { portsFor, type Link, type Partners } from '@bifrost/ui/city/links'
import { useRulersTick } from '@bifrost/ui/city/rulers'

// The gen2 frame on its own: the address is the place (#/system/infrastructure), so Back works.
// The content plug-in: the domains the map reads, and the hardware district read live through the platform
// (the bench's /api proxy: PLATFORM_API_TARGET), every 30 s.
const getJson = (p: string) => fetch(p).then(r => (r.ok ? r.json() : Promise.reject(new Error(`${p}: HTTP ${r.status}`))))

/**
 * Bench only, ?links=1: made-up connections between the Console's districts, to look at the two ways
 * (links.ts) before the real ones are read from the domains' contracts. Never in the Console.
 */
const SAMPLE = 'bench-sample-links.json'
const SAMPLE_LINKS: Link[] = [
  { way: 'bus', from: ['work', 'releases'], to: ['command', 'command'], what: 'release.finished', source: SAMPLE },
  { way: 'bus', from: ['work', 'releases'], to: ['command', 'records'], what: 'release.recorded', source: SAMPLE },
  { way: 'bus', from: ['command', 'command'], to: ['work', 'releases'], what: 'approval.granted', source: SAMPLE },
  { way: 'bus', from: ['services', 'ib'], to: ['services', 'data'], what: 'quotes.arrived', source: SAMPLE },
  { way: 'bus', from: ['system', 'bus'], to: ['work', 'work'], what: 'ci.run.finished', source: SAMPLE },
  { way: 'bus', from: ['work', 'work'], to: ['work', 'releases'], what: 'ci.passed', source: SAMPLE },
  { way: 'metric', from: ['services', 'data'], to: ['system', 'status'], what: 'ingest_rows_total', source: SAMPLE },
  { way: 'metric', from: ['services', 'ib'], to: ['system', 'status'], what: 'ib_connected', source: SAMPLE },
  { way: 'metric', from: ['crew', 'crew'], to: ['system', 'status'], what: 'seat_heartbeat', source: SAMPLE },
  { way: 'bus', from: ['system', 'status'], to: ['command', 'records'], what: 'status.changed', source: SAMPLE },
  { way: 'metric', from: ['system', 'infrastructure'], to: ['utilities'], what: 'node_cpu_seconds_total', source: SAMPLE },
  { way: 'bus', from: ['system', 'bus'], to: ['command', 'command'], what: 'loop.health.changed', source: SAMPLE, clarity: 'unclear' },
  ...['network', 'telemetry', 'alertrelay', 'selfhealth', 'catalog', 'checklist', 'mcp'].map((what): Link => ({ way: 'bus', from: ['system', 'infrastructure'], to: [], what, source: SAMPLE, clarity: 'orphan' })),
  { way: 'bus', from: ['crew', 'crew'], to: ['work', 'releases'], what: 'release.requested', source: SAMPLE },
  // Bench only: traffic in and out of Utilities through its named ports and its Other (2026-11-20).
  { way: 'metric', from: ['utilities', 'ubt-k3s-04'], to: ['system', 'status'], what: 'node_load1', source: SAMPLE },
  { way: 'metric', from: ['utilities', 'mini-pc-a'], to: ['command', 'command'], what: 'node_memory_used', source: SAMPLE },
  { way: 'bus', from: ['utilities', 'gpu-server'], to: ['work', 'work'], what: 'gpu.idle', source: SAMPLE },
  { way: 'bus', from: ['work', 'releases'], to: ['utilities', 'mini-pc-c'], what: 'deploy.started', source: SAMPLE },
  { way: 'bus', from: ['services', 'ib'], to: ['utilities', 'UGREEN-NAS'], what: 'snapshot.written', source: SAMPLE },
  { way: 'bus', from: ['crew', 'crew'], to: ['utilities', 'ubt-k3s-06'], what: 'agent.started', source: SAMPLE },
]
/** Bench only: whom each place declares as partners (the content's rules), so the edge has named ports. */
const SAMPLE_PARTNERS: Record<string, Partners> = {
  work: { in: ['command', 'system', 'crew'], out: ['command'] },
  system: { in: ['crew', 'services'], out: ['work', 'command', 'utilities'] },
  command: { in: ['work'], out: ['work'] },
  services: { in: [], out: ['system'] },
  // Up to four a side, so a 4×8 Utilities (?util=4x8) shows every port it has named (2026-11-20).
  utilities: { in: ['system', 'work', 'services', 'crew'], out: ['system', 'command', 'work', 'services'] },
}
// No more partners than the place has ports a side (the frame's rule): the sample is cut to the size shown.
const withPartners = (p: Place): Place => {
  const d = SAMPLE_PARTNERS[p.id], n = portsFor(p.shape)
  return { ...p, partners: d ? { in: d.in.slice(0, n), out: d.out.slice(0, n) } : p.partners, children: p.kind === 'town' ? p.children.map(withPartners) : p.children }
}

function Bench() {
  const [reads, setReads] = useState<{ r: Awaited<ReturnType<typeof readHardware>>; u: Awaited<ReturnType<typeof readUnits>>; at: string } | null>(null)
  useEffect(() => {
    let on = true
    // The hardware, and every unit's details for the rooms drawn by their modules (CPU, memory, disk).
    const tick = () => Promise.all([readHardware(getJson), readUnits(getJson)]).then(([r, u]) => { if (on && r.topology) setReads({ r, u, at: new Date().toLocaleTimeString() }) })
    tick()
    const t = setInterval(tick, 30_000)
    return () => { on = false; clearInterval(t) }
  }, [])
  // The map line's lights; the bench proxy carries no viewer token, so here it shows the not-known path.
  const [domains, setDomains] = useState<DomainsRead>({})
  useEffect(() => {
    let on = true
    // Bench only: without a token the domains come from the ownership table itself, every light unknown.
    const offline = (): DomainsRead => ({ data: { version: 0, note: 'bench: from config/domain-ownership.json; no light read', domains: OWNERSHIP.domains.map(d => ({ id: d.id, name: d.name, overall: '', alive: NO_LIGHT, interfaces: NO_LIGHT, boundary: NO_LIGHT, fresh: NO_LIGHT })) } as DomainsRead['data'] })
    const tick = () => getJson('/api/v1/catalog/status').then(d => { if (on) setDomains({ data: d as DomainStatusResponse }) }, () => { if (on) setDomains(offline()) })
    tick()
    const t = setInterval(tick, 30_000)
    return () => { on = false; clearInterval(t) }
  }, [])
  // Who stands on whom: read through the proxy; without a token, bench only, the graph measured on platform main
  // 9d9fd3e2 (2026-11-20) from a snapshot, said so in its note.
  const [graph, setGraph] = useState<GraphRead>({})
  useEffect(() => {
    let on = true
    getJson(`/api/v1/ci/domains?repo=${GRAPH_REPO}`).then(g => { if (on) setGraph({ data: g as CiDomainGraph }) }, () => { if (on) setGraph({ data: GRAPH_SNAPSHOT as CiDomainGraph }) })
    return () => { on = false }
  }, [])
  const rulers = useRulersTick()
  const CONTENT = useMemo(() => {
    void rulers
    // The rooms are drawn under the rulers as they stand, so the district is built again when one changes.
    const hw: Place | null = reads ? hardwareDistrict(reads.r, 'system', `Live, read every 30 s (last ${reads.at}).`, reads.u) : null
    // Bench only, ?util=4x8 or 8x8: Utilities at another size, to look at how a district's town grows (2026-11-20).
    const util = new URLSearchParams(location.search).get('util')
    if (hw && util && ['4x4', '4x8', '8x8'].includes(util)) hw.shape = util as Place['shape']
    const c = consoleContent(domains, hw, graph)
    return new URLSearchParams(location.search).has('links') ? { ...c, tree: withPartners(c.tree), links: SAMPLE_LINKS } : c
  }, [reads, rulers, domains, graph])
  const [path, setPath] = useState<Path>(() => parsePath(location.hash))
  useEffect(() => {
    const on = () => setPath(parsePath(location.hash))
    addEventListener('hashchange', on)
    return () => removeEventListener('hashchange', on)
  }, [])
  // The dashboard look of a place is the Console itself, at the same place.
  return <CityGen2 content={CONTENT} path={path} go={p => { location.hash = formatPath(p) }} consoleHref={p => p.dashboard && `/${p.dashboard}`} timerLoop={new URLSearchParams(location.search).has('timer')} />
}

createRoot(document.getElementById('root')!).render(<Bench />)
