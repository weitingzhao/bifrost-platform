import { useEffect, useState } from 'react'
import CityGen2 from '@bifrost/ui/city/CityGen2'
import { cityHref, consoleHrefOf, readCityQuery } from '@/domains/owner/city/gen2/content/looks'
import { useCityContent } from './useCityContent'

const queryOf = () => window.location.hash.split('?')[1] ?? ''

/**
 * The city look: the whole screen, outside the Console's frame (the Owner, 2026-11-19: the game look
 * and the dashboard look are peers; the dashboard does not hold the game). Its switch City | Console
 * opens the same place in the Console, and the Console's switch brings it back here.
 */
export default function CityScreen() {
  const [q, setQ] = useState(() => readCityQuery(queryOf()))
  // A line picked in the Console (sel=<session>) comes first on the Lines card; a room id is not a session and matches no row.
  const content = useCityContent(q.sel)
  useEffect(() => {
    const on = () => setQ(readCityQuery(queryOf()))
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  return (
    <div style={{ position: 'fixed', inset: 0, zIndex: 40 }}>
      <CityGen2 content={content} path={q.path} picked={q.sel ?? undefined}
        go={p => { window.location.hash = cityHref(p) }} consoleHref={consoleHrefOf} otherLooks={[{ label: 'Agents', href: '#collaboration' }]} />
    </div>
  )
}
