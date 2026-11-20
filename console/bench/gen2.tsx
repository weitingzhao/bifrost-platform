import { useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import CityGen2 from '../src/domains/owner/city/gen2/frame/CityGen2'
import { consoleContent } from '../src/domains/owner/city/gen2/content/console'
import { formatPath, parsePath, type Path, type Place } from '../src/domains/owner/city/gen2/frame/places'
import { hardwareDistrict, readHardware } from '../src/domains/owner/city/gen2/content/hardware'
import { readUnits } from '../src/domains/owner/city/gen2/content/unitRooms'

// The gen2 frame on its own: the address is the place (#/system/infrastructure), so Back works.
// The content plug-in: the Console's menu, and the hardware district read live through the platform
// (the bench's /api proxy: PLATFORM_API_TARGET), every 30 s.
const getJson = (p: string) => fetch(p).then(r => (r.ok ? r.json() : Promise.reject(new Error(`${p}: HTTP ${r.status}`))))

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
  const CONTENT = useMemo(() => consoleContent(hw ? [hw] : []), [hw])
  const [path, setPath] = useState<Path>(() => parsePath(location.hash))
  useEffect(() => {
    const on = () => setPath(parsePath(location.hash))
    addEventListener('hashchange', on)
    return () => removeEventListener('hashchange', on)
  }, [])
  // The dashboard look of a place is the Console itself, at the same place.
  return <CityGen2 content={CONTENT} path={path} go={p => { location.hash = formatPath(p) }} dashboardHref={p => p.dashboard && `/${p.dashboard}`} timerLoop={new URLSearchParams(location.search).has('timer')} />
}

createRoot(document.getElementById('root')!).render(<Bench />)
