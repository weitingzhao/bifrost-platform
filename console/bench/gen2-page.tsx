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

// The content plug-in: the Console's menu, and the hardware district from one read kept at build time
// (bench/export-hardware.mjs; the page cannot reach the LAN), labelled with when it was read.
const snap = Object.values(import.meta.glob('./hardware.local.json', { eager: true, import: 'default' }))[0] as (HardwareReads & { read_at: string }) | undefined
const CONTENT = consoleContent(snap?.topology ? [hardwareDistrict(snap, 'system', `A snapshot read ${new Date(snap.read_at).toLocaleString()}, not live.`)] : [])

function Page() {
  const [path, setPath] = useState<Path>(() => parse(location.hash))
  useEffect(() => {
    const on = () => setPath(parse(location.hash))
    addEventListener('hashchange', on)
    return () => removeEventListener('hashchange', on)
  }, [])
  return <CityGen2 content={CONTENT} path={path} go={p => { location.hash = format(p) }} dashboardHref={() => null} />
}

createRoot(document.getElementById('root')!).render(<Page />)
