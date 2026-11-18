/**
 * The city builder on the bench: the system's domains and release lanes from
 * the local snapshot, the layout in this browser. ?fresh=1 starts from bare
 * land, ?seed=<name> a saved layout file. The real page reads the same things from the platform.
 */
import { createRoot } from 'react-dom/client'
import type { TiledMap } from '../src/domains/owner/city/builderDoc'
import CityBuilder from '../src/domains/owner/city/CityBuilder'
import { browserLayoutStore, LAYOUT_KEY, type LayoutStore } from '../src/domains/owner/city/layoutStore'
import { systemItems } from '../src/domains/owner/city/catalog'

const q = new URLSearchParams(location.search)
if (q.has('fresh')) { try { localStorage.removeItem(LAYOUT_KEY) } catch { /* no storage: starts bare anyway */ } }
const browser = browserLayoutStore()
// ?seed=<name> opens a layout saved beside the snapshot (a headless browser has no earlier session).
const seed = q.get('seed')
const store: LayoutStore = seed ? { ...browser, load: async () => await (await fetch(`./layout-${seed}.local.json`)).json() as TiledMap } : browser
void fetch('./snapshot.local.json').then(r => r.json()).then(s => {
  const items = systemItems(s.tiers ?? [], s.lanes ?? [], ['market-data', 'flex-query', 'ib-gateway'])
  createRoot(document.getElementById('root')!).render(<CityBuilder items={items} store={store} startEditing={q.has('edit')} onOpen={(it, p) => { document.title = `open ${it?.page ?? p.binds}` }} />)
})
