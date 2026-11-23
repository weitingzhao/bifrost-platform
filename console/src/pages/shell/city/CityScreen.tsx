import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import CityGen2 from '@bifrost/ui/city/CityGen2'
import { WorldFrame } from '@bifrost/ui/city/WorldFrame'
import type { Place } from '@bifrost/ui/city/places'
import { cityHref, consoleHrefOf, ledgerHref, readCityQuery } from '@/domains/owner/city/gen2/content/looks'
import { crumbsOf, frameAlerts } from '@/domains/owner/city/gen2/content/frame'
import { useShellStatusLine } from '@/domains/foundation/shell/useShellStatusLine'
import { viewerEnvBadgeLabel } from '@/domains/foundation/observation-display/fleetNavigation'
import { useCityContent } from './useCityContent'
import './cityScreen.css'

const queryOf = () => window.location.hash.split('?')[1] ?? ''

/** The place the address names, as far as it resolves (the frame's Console link opens this one). */
function placeAt(tree: Place, path: readonly string[]): Place {
  let at = tree
  for (const id of path) {
    const next = at.children.find(c => c.id === id)
    if (!next) break
    at = next
  }
  return at
}

/**
 * The city look: the whole screen, outside the Console's frame (the Owner, 2026-11-19: the game look and the
 * dashboard look are peers; the dashboard does not hold the game), under the one world frame every view wears
 * (箭10 2026-11-23, cockpit/ui-world/frame): where you are, the four places in the engine's order, the environment,
 * the time and the alerts corner. The scene's own look switch and alerts corner give way to the frame; its own
 * tools (Arrange, Labels, light) stay in the scene.
 */
export default function CityScreen() {
  const [q, setQ] = useState(() => readCityQuery(queryOf()))
  // A line picked in the Console (sel=<session>) comes first on the Lines card; a room id is not a session and matches no row.
  const content = useCityContent(q.sel)
  const [alertsOpen, setAlertsOpen] = useState(false)
  const status = useShellStatusLine()
  // The hardware's last reading (the query useCityContent runs): the frame's live time.
  const hardware = useQuery<{ at: number }>({ queryKey: ['city', 'gen2', 'hardware'], enabled: false })
  useEffect(() => {
    const on = () => setQ(readCityQuery(queryOf()))
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  const place = useMemo(() => placeAt(content.tree, q.path), [content.tree, q.path])
  const at = hardware.data?.at
  return (
    <div className="city-screen">
      <WorldFrame
        here="city"
        crumbs={crumbsOf(content.tree, q.path)}
        hrefs={{ city: cityHref(q.path, q.sel), signals: '#observation', agents: '#collaboration', console: consoleHrefOf(place, q.path, null) }}
        env={status.viewerEnvLoading ? undefined : viewerEnvBadgeLabel(status.viewerEnv)}
        time={{ mode: 'live', ...(at ? { at: new Date(at).toISOString() } : {}) }}
        alerts={{ ...frameAlerts(content.alerts), onOpen: () => setAlertsOpen(true) }}
        looks={[{ id: 'map', label: 'Map', href: cityHref(q.path, q.sel), on: true }, { id: 'table', label: 'Table', href: ledgerHref(q.path, q.sel), on: false }]}
      />
      <div className="city-screen-body">
        <CityGen2 content={content} path={q.path} picked={q.sel ?? undefined}
          go={p => { window.location.hash = cityHref(p) }} consoleHref={() => null}
          alerts={{ corner: false, open: alertsOpen, onOpenChange: setAlertsOpen }} />
      </div>
    </div>
  )
}
