import { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import CityGen2 from '../src/domains/owner/city/gen2/frame/CityGen2'
import { consoleContent } from '../src/domains/owner/city/gen2/content/console'
import type { Path } from '../src/domains/owner/city/gen2/frame/places'

// The gen2 frame as one self-contained page, to open on a phone (published as an Artifact).
// Its host passes only a plain #name in the address, so a place is #system--infrastructure; the town is no hash.
// No Console sits behind it, so there is no dashboard link.
const parse = (h: string): Path => h.replace(/^#/, '').split('--').filter(Boolean)
const format = (p: Path) => (p.length === 0 ? ' ' : `#${p.join('--')}`)

// The content plug-in: today the Console's menu.
const CONTENT = consoleContent()

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
