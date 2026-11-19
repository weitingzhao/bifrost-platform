import { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import CityGen2 from '../src/domains/owner/city/gen2/CityGen2'
import type { Path } from '../src/domains/owner/city/gen2/places'

// The gen2 frame as one self-contained page, to open on a phone (published as an Artifact).
// Its host passes only a plain #name in the address, so a place is #system--infrastructure; the town is no hash.
// No Console sits behind it, so there is no dashboard link.
const parse = (h: string): Path => h.replace(/^#/, '').split('--').filter(Boolean)
const format = (p: Path) => (p.length === 0 ? ' ' : `#${p.join('--')}`)

function Page() {
  const [path, setPath] = useState<Path>(() => parse(location.hash))
  useEffect(() => {
    const on = () => setPath(parse(location.hash))
    addEventListener('hashchange', on)
    return () => removeEventListener('hashchange', on)
  }, [])
  return <CityGen2 path={path} go={p => { location.hash = format(p) }} dashboardHref={() => null} />
}

createRoot(document.getElementById('root')!).render(<Page />)
