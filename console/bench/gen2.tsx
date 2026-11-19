import { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import CityGen2 from '../src/domains/owner/city/gen2/frame/CityGen2'
import { consoleContent } from '../src/domains/owner/city/gen2/content/console'
import { formatPath, parsePath, type Path } from '../src/domains/owner/city/gen2/frame/places'

// The gen2 frame on its own: the address is the place (#/system/infrastructure), so Back works.
// The content plug-in: today the Console's menu. The frame takes any other the same way.
const CONTENT = consoleContent()

function Bench() {
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
