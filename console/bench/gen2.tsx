import { useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import CityGen2 from '../src/domains/owner/city/gen2/frame/CityGen2'
import { consoleContent } from '../src/domains/owner/city/gen2/content/console'
import { formatPath, parsePath, type Path, type Place } from '../src/domains/owner/city/gen2/frame/places'
import { hardwareDistrict, readHardware } from '../src/domains/owner/city/gen2/content/hardware'
import { readUnits } from '../src/domains/owner/city/gen2/content/unitRooms'
import { portsFor, type Link, type Partners } from '../src/domains/owner/city/gen2/frame/links'
import { useRulersTick } from '../src/domains/owner/city/gen2/frame/rulers'

// The gen2 frame on its own: the address is the place (#/system/infrastructure), so Back works.
// The content plug-in: the Console's menu, and the hardware district read live through the platform
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
  const rulers = useRulersTick()
  const CONTENT = useMemo(() => {
    void rulers
    // The rooms are drawn under the rulers as they stand, so the district is built again when one changes.
    const hw: Place | null = reads ? hardwareDistrict(reads.r, 'system', `Live, read every 30 s (last ${reads.at}).`, reads.u) : null
    // Bench only, ?util=4x8 or 8x8: Utilities at another size, to look at how a district's town grows (2026-11-20).
    const util = new URLSearchParams(location.search).get('util')
    if (hw && util && ['4x4', '4x8', '8x8'].includes(util)) hw.shape = util as Place['shape']
    const c = consoleContent(hw ? [hw] : [])
    return new URLSearchParams(location.search).has('links') ? { ...c, tree: withPartners(c.tree), links: SAMPLE_LINKS } : c
  }, [reads, rulers])
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
