/**
 * The city builder on the bench: the system's domains and release lanes from
 * the local snapshot, the layout in this browser. ?fresh=1 starts from the
 * plan, ?seed=<name> a saved layout file, &edit=1 builds. ?demo=1 feeds the
 * real-state readers made-up release contexts (bench only), to look at the
 * hangars, the lit legs, a launch and the satellites; the page reads the platform.
 */
import { createRoot } from 'react-dom/client'
import type { TiledMap } from '../src/domains/owner/city/builderDoc'
import CityBuilder, { type CityReading } from '../src/domains/owner/city/CityBuilder'
import { systemItems, type CityLane } from '../src/domains/owner/city/catalog'
import { padReading, rocketCiReading, rocketStgReading, satellitesReading, skyReading, waitingReading, type ReleaseContext } from '../src/domains/owner/city/cityReadings'
import { plannedCity } from '../src/domains/owner/city/defaultLayout'
import { browserLayoutStore, LAYOUT_KEY, type LayoutStore } from '../src/domains/owner/city/layoutStore'

const q = new URLSearchParams(location.search)
if (q.has('fresh')) { try { localStorage.removeItem(LAYOUT_KEY) } catch { /* no storage: starts from the plan anyway */ } }
const browser = browserLayoutStore()
const seed = q.get('seed')
const store: LayoutStore = seed ? { ...browser, load: async () => await (await fetch(`./layout-${seed}.local.json`)).json() as TiledMap } : browser

void fetch('./snapshot.local.json').then(r => r.json()).then(s => {
  const lanes: CityLane[] = s.lanes ?? []
  const items = systemItems(s.tiers ?? [], lanes, [])
  const rel = { commit: 'f6815367aa', run: 'r', completed_at: new Date(Date.now() - 3 * 3600_000).toISOString() }
  const ctx = (repo: string, runs: unknown[]): ReleaseContext => ({ repo, facts: { runs_in_flight: { value: runs as never }, prod: { value: rel }, stg: { value: rel }, window: { value: { held: false, who: '', what: '' } } } })
  const demo = q.has('demo')
  const contexts = new Map<string, ReleaseContext | null>(demo ? [
    ['bifrost-platform', ctx('bifrost-platform', [{ name: 'ci-1', pipeline: 'bifrost-ci-platform' }, { name: 'ci-2', pipeline: 'bifrost-ci-platform' }, { name: 'land-1', pipeline: 'bifrost-land' }, { name: 'dp-1', pipeline: 'bifrost-deliver-platform', delivery: true }])],
    ['bifrost-trade-api', ctx('bifrost-trade-api', [{ name: 'prod-1', pipeline: 'bifrost-deliver-prod', delivery: true }, { name: 'py-1', pipeline: 'bifrost-ci-python' }])],
    ['bifrost-research', ctx('bifrost-research', [])],
  ] : [])
  const now = Date.now()
  const readings = new Map<string, CityReading>(demo ? [
    ['stage:rocket-ci', { ...rocketCiReading(contexts, lanes), source: 'demo' }],
    ['stage:rocket-stg', { ...rocketStgReading(contexts, lanes, now), source: 'demo' }],
    ['stage:satellites', { ...satellitesReading(contexts, lanes), source: 'demo' }],
    ['lane:bifrost-deliver-platform-prod', { ...padReading(contexts, lanes, now), source: 'demo' }],
    ['domain:owner', { ...waitingReading(1, 1), source: 'demo' }],
  ] : [])
  const sky = demo ? skyReading(contexts, lanes, now) : undefined
  createRoot(document.getElementById('root')!).render(<CityBuilder items={items} store={store} startEditing={q.has('edit')} initial={plannedCity}
    readingOf={b => readings.get(b)} sky={sky} readingsKey={demo ? 'demo' : ''} onOpen={(it, p) => { document.title = `open ${it?.page ?? p.binds}` }} />)
})
