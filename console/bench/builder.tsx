/**
 * The city builder on the bench: the system's domains and release lanes from
 * the local snapshot, the layout in this browser. ?fresh=1 starts from the
 * plan, ?seed=<name> a saved layout file, &edit=1 builds. ?demo=1 feeds the
 * real-state readers made-up release contexts (bench only), to look at the
 * hangars, the lit legs, a launch and the satellites; the page reads the platform.
 */
import { createRoot } from 'react-dom/client'
import type { TiledMap } from '../src/domains/owner/city/builderDoc'
import CityBuilder from '../src/domains/owner/city/CityBuilder'
import { pluginsOf, systemItems, type CityLane } from '../src/domains/owner/city/catalog'
import { skyReading, type ReleaseContext } from '../src/domains/owner/city/cityReadings'
import { cityReadings, utilityGlance } from '../src/domains/owner/city/cityState'
import { workersGlance, workersOf } from '../src/domains/owner/city/cityWorkers'
import { campuses, DEPLOYMENTS_QUERY } from '../src/domains/owner/city/cityCampus'
import { FLIGHT_PIPELINES, flightLog, type Run } from '../src/domains/owner/city/cityFlights'
import { readUtilities } from '../src/domains/owner/city/cityUtilities'
import { machines, variantOf } from '../src/domains/owner/city/cityMachines'
import { fetchPromQL } from '../src/api/telemetry'
import { plannedCity } from '../src/domains/owner/city/defaultLayout'
import { browserLayoutStore, LAYOUT_KEY, type LayoutStore } from '../src/domains/owner/city/layoutStore'

const q = new URLSearchParams(location.search)
if (q.has('fresh')) { try { localStorage.removeItem(LAYOUT_KEY) } catch { /* no storage: starts from the plan anyway */ } }
const browser = browserLayoutStore()
const seed = q.get('seed')
const store: LayoutStore = seed ? { ...browser, load: async () => await (await fetch(`./layout-${seed}.local.json`)).json() as TiledMap } : browser

// The utilities are real: the dev server forwards /api to PLATFORM_API_TARGET and PromQL needs no sign-in.
void Promise.all([fetch('./snapshot.local.json').then(r => r.json()), readUtilities(async query => (await fetchPromQL(query)).points).catch(() => null), fetchPromQL(DEPLOYMENTS_QUERY).then(r => r.points, () => null), fetch('/api/v1/topology?env=prod').then(r => r.ok ? r.json() : null).catch(() => null)]).then(async ([s, util, deployments, topo]) => {
  const lanes: CityLane[] = s.lanes ?? []
  // The flight log is real: the pipelines' runs need no sign-in.
  const runs = (await Promise.all(FLIGHT_PIPELINES(lanes).map(name => fetch(`/api/v1/delivery/pipelines/${name}/runs`).then(r => r.ok ? r.json() : { runs: [] }).catch(() => ({ runs: [] }))))).flatMap(b => (b.runs ?? []) as Run[])
  const ms = machines(util?.info ?? null, util?.capacity ?? null, topo?.nodes ?? null)
  const nodes = ms.map(m => ({ node: m.id, cores: m.cores, name: m.name, variant: variantOf(m), offGrid: m.offGrid }))
  const items = systemItems(s.tiers ?? [], lanes, pluginsOf(lanes), nodes)
  const rel = { commit: 'f6815367aa', run: 'r', completed_at: new Date(Date.now() - 3 * 3600_000).toISOString() }
  const ctx = (repo: string, runs: unknown[]): ReleaseContext => ({ repo, facts: { runs_in_flight: { value: runs as never }, prod: { value: rel }, stg: { value: rel }, window: { value: { held: false, who: '', what: '' } } } })
  const demo = q.has('demo')
  const contexts = new Map<string, ReleaseContext | null>(demo ? [
    ['bifrost-platform', ctx('bifrost-platform', [{ name: 'ci-1', pipeline: 'bifrost-ci-platform' }, { name: 'ci-2', pipeline: 'bifrost-ci-platform' }, { name: 'land-1', pipeline: 'bifrost-land' }, { name: 'dp-1', pipeline: 'bifrost-deliver-platform', delivery: true }])],
    ['bifrost-trade-api', ctx('bifrost-trade-api', [{ name: 'prod-1', pipeline: 'bifrost-deliver-prod', delivery: true }, { name: 'py-1', pipeline: 'bifrost-ci-python' }])],
    ['bifrost-research', ctx('bifrost-research', [])],
    ['bifrost-trade-core', ctx('bifrost-trade-core', [{ name: 'py-2', pipeline: 'bifrost-ci-python' }])],
    ['bifrost-finance', ctx('bifrost-finance', [])],
    ['bifrost-platform-plugin-market-data', ctx('bifrost-platform-plugin-market-data', [{ name: 'md-1', pipeline: 'bifrost-build-market-data' }])],
    ['bifrost-platform-plugin', ctx('bifrost-platform-plugin', [])],
  ] : [])
  const now = Date.now()
  const readings = cityReadings({ contexts, lanes, items, util, offGrid: ms.filter(m => m.offGrid), approvals: demo ? 1 : null, questions: demo ? 1 : null, now })
  const sky = demo ? skyReading(contexts, lanes, now) : undefined
  // Bench only: which namespaces each tenant runs in (the tenant domain's contract will say); the deployments are read for real.
  const NS: Record<string, string[]> = { Trade: ['bifrost-prod'], Research: ['research'], Finance: ['finance-prod'] }
  const camps = campuses(lanes, contexts, deployments, demo ? t => NS[t] : undefined)
  const ago = (m: number) => new Date(now - m * 60_000).toISOString()
  const seat = (id: string, charter: string, at: string, doing: string, waiting_on?: string) => ({ seat: `thread:local_${id}`, message_id: id, at, charter, goal: '', doing, percent: 50, percent_basis: 'steps', next: 'next step', waiting_on })
  // Bench only: the seats route needs a viewer token the bench does not hold.
  const workers = demo ? workersOf([
    seat('a1', '3-4-verify-release-ci', ago(5), 'landing the CI queue fix'),
    seat('a2', '6-compute', ago(12), 'spreading CI off node 01'),
    seat('a3', '10-cockpit', ago(2), 'the city: seats walk'),
    seat('a4', '8-2-comm-desk', ago(30), 'doorbell replies', 'the Owner'),
    seat('a5', '2-lineage', ago(400), 'commit lineage'),
    seat('a6', '7-tenant', ago(20), 'research tenant contract'),
    seat('a7', 'company-coordinator', ago(8), 'four lines in step'),
  ], now) : undefined
  createRoot(document.getElementById('root')!).render(<CityBuilder items={items} store={store} startEditing={q.has('edit')} initial={plannedCity}
    readingOf={b => readings.get(b)} sky={sky} readingsKey={demo ? 'demo' : 'real'} workers={workers} campusOf={b => camps.get(b.replace(/^tenant:/, ''))} flights={flightLog(runs, lanes, Date.now())} glance={[utilityGlance(util), workersGlance(workers)]} onOpen={(it, p) => { document.title = `open ${it?.page ?? p.binds}` }} />)
})
