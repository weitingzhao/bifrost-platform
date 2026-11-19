import { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import CityGen2 from '../src/domains/owner/city/gen2/frame/CityGen2'
import { consoleContent } from '../src/domains/owner/city/gen2/content/console'
import type { Path } from '../src/domains/owner/city/gen2/frame/places'
import { hardwareDistrict, type HardwareReads } from '../src/domains/owner/city/gen2/content/hardware'

// The gen2 frame as one self-contained page, to open on a phone (published as an Artifact).
// Its host passes only a plain #name in the address, so a place is #system--infrastructure; the town is no hash.
// No Console sits behind it, so there is no dashboard link.
const parse = (h: string): Path => h.replace(/^#/, '').split('--').filter(Boolean)
const format = (p: Path) => (p.length === 0 ? ' ' : `#${p.join('--')}`)

// The content plug-in: the Console's menu, and the hardware district. This page cannot reach the LAN,
// and a number measured once and left on the screen is made up (the Owner, 2026-11-19): so it keeps
// only which devices the topology listed at build time, and every reading shows unread. Live readings
// are on the LAN (bench/gen2.html with PLATFORM_API_TARGET, or the Console).
const snap = Object.values(import.meta.glob('./hardware.local.json', { eager: true, import: 'default' }))[0] as (HardwareReads & { read_at: string }) | undefined
const unread = (r: HardwareReads): HardwareReads => ({ topology: r.topology, prom: Object.fromEntries(Object.keys(r.prom).map(k => [k, null])) as HardwareReads['prom'] })
const LISTED = snap ? new Date(snap.read_at).toLocaleString() : ''
const CONTENT = consoleContent(snap?.topology ? [hardwareDistrict(unread(snap), 'system', `Devices as the topology listed them at ${LISTED}.`)] : [])
const NOTICE = 'This page cannot reach the system: every reading shows unread. Live readings are on the LAN.'

function Page() {
  const [path, setPath] = useState<Path>(() => parse(location.hash))
  useEffect(() => {
    const on = () => setPath(parse(location.hash))
    addEventListener('hashchange', on)
    return () => removeEventListener('hashchange', on)
  }, [])
  return <CityGen2 content={CONTENT} path={path} go={p => { location.hash = format(p) }} dashboardHref={() => null} notice={NOTICE} />
}

createRoot(document.getElementById('root')!).render(<Page />)
