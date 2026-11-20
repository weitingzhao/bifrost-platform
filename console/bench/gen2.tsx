import { useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import CityGen2 from '../src/domains/owner/city/gen2/frame/CityGen2'
import { consoleContent } from '../src/domains/owner/city/gen2/content/console'
import { formatPath, parsePath, type Path, type Place } from '../src/domains/owner/city/gen2/frame/places'
import { hardwareDistrict, readHardware } from '../src/domains/owner/city/gen2/content/hardware'
import { readUnits } from '../src/domains/owner/city/gen2/content/unitRooms'
import { OUTSIDE, type Link, type Partners } from '../src/domains/owner/city/gen2/frame/links'

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
  { way: 'bus', from: [OUTSIDE, 'console'], to: ['command', 'command'], what: 'decision.made', source: SAMPLE },
  { way: 'bus', from: [OUTSIDE, 'mcp'], to: ['work', 'releases'], what: 'release.requested', source: SAMPLE },
  { way: 'metric', from: ['system', 'status'], to: [OUTSIDE, 'console'], what: 'status_up', source: SAMPLE },
  { way: 'bus', from: [OUTSIDE, 'trade'], to: ['services', 'ib'], what: 'order.intent', source: SAMPLE },
]
const SAMPLE_OUTSIDE = [{ id: 'console', label: 'Console' }, { id: 'mcp', label: 'MCP' }, { id: 'trade', label: 'Trade' }]
/** Bench only: whom each place declares as partners (the content's rules), so the edge has named ports. */
const SAMPLE_PARTNERS: Record<string, Partners> = {
  town: { in: ['~/console', '~/mcp', '~/trade'], out: ['~/console'] },
  work: { in: ['command', 'system', '~'], out: ['command'] },
  system: { in: ['crew', 'services'], out: ['work', 'command', 'utilities'] },
  command: { in: ['work', '~'], out: ['work'] },
  services: { in: ['~'], out: ['system'] },
}
const withPartners = (p: Place): Place => ({ ...p, partners: SAMPLE_PARTNERS[p.id] ?? p.partners, children: p.kind === 'town' ? p.children.map(withPartners) : p.children })

function Bench() {
  const [hw, setHw] = useState<Place | null>(null)
  useEffect(() => {
    let on = true
    // The hardware, and every unit's details for the rooms drawn by their modules (CPU, memory, disk).
    const tick = () => Promise.all([readHardware(getJson), readUnits(getJson)]).then(([r, u]) => { if (on && r.topology) setHw(hardwareDistrict(r, 'system', `Live, read every 30 s (last ${new Date().toLocaleTimeString()}).`, u)) })
    tick()
    const t = setInterval(tick, 30_000)
    return () => { on = false; clearInterval(t) }
  }, [])
  const CONTENT = useMemo(() => {
    const c = consoleContent(hw ? [hw] : [])
    return new URLSearchParams(location.search).has('links') ? { ...c, tree: withPartners(c.tree), links: SAMPLE_LINKS, outside: SAMPLE_OUTSIDE } : c
  }, [hw])
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
