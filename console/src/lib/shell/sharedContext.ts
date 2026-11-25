import { useSyncExternalStore } from 'react'
import { worldLocation } from '@/components/shell/world-journey/location'

/*
 * The context the views share (箭10 2026-11-25, gap ③ of the progress score; design
 * cockpit/ui-console/shared-context-design.md, reviewed): switching place keeps what you were looking at instead of
 * landing on the place's home. Three things only: a scope (one domain), a focus (one thing, kind and id) and a time
 * (live, or a moment pinned in Signals). A page's own filters, sort and windows stay on that page.
 *
 * The address is the only truth. Each place keeps its own parameters and registers an adapter: toContext reads its
 * address into the context, hrefFor builds its address from a context. The shell notes the context of every address
 * it passes (sessionStorage, one per tab, as the journey trail) and the frames' place links are built from it. A focus
 * the target cannot show is not dropped quietly: its address carries from=<kind>:<id> and the frame says so.
 */

export type Place = 'city' | 'signals' | 'agents' | 'console'
export type FocusKind = 'lane' | 'line' | 'node' | 'building' | 'letter' | 'step'
export type Focus = { kind: FocusKind; id: string }
export type WorldTime = { mode: 'live' } | { mode: 'at'; at: string }
export type WorldContext = { scope?: string; focus?: Focus; time?: WorldTime }

/** What a place's address says, and the address a context opens there (null: the place has nothing for it). */
export type ContextAdapter = {
  toContext: (hash: string) => WorldContext
  /** shown: whether the focus is shown at that address; a focus not shown travels as from=. */
  hrefFor: (ctx: WorldContext) => { href: string; shown: boolean } | null
}

/** Each place's home: a frame link that would open only this is where context fills in. */
export const PLACE_HOME: Record<Place, string> = { city: '#city', signals: '#observation', agents: '#collaboration', console: '#command' }

const adapters = new Map<Place, ContextAdapter>()
/** A view registers its own adapter (its line's file); a place without one keeps its link as the host gives it. */
export function registerContextAdapter(place: Place, adapter: ContextAdapter): void {
  adapters.set(place, adapter)
}
/** For tests. */
export function clearContextAdapters(): void {
  adapters.clear()
}

export function placeOf(hash: string): Place {
  const p = worldLocation(hash).place
  return p === 'City' || p === 'Domain ledger' ? 'city' : p === 'Agents' || p === 'Communication ledger' ? 'agents' : p === 'Signals' ? 'signals' : 'console'
}

const FOCUS_KINDS: readonly FocusKind[] = ['lane', 'line', 'node', 'building', 'letter', 'step']
export const focusText = (f: Focus) => `${f.kind}:${f.id}`
export function focusOf(text: string | null | undefined): Focus | undefined {
  const m = /^([a-z]+):(.+)$/.exec(text ?? '')
  return m && (FOCUS_KINDS as readonly string[]).includes(m[1]) ? { kind: m[1] as FocusKind, id: m[2] } : undefined
}

// --- the store: the last context noted, one per tab ---------------------------------------------------------------

const KEY = 'bifrost.world.context'
const listeners = new Set<() => void>()
let memory = '{}'
const raw = (): string => { try { return sessionStorage.getItem(KEY) ?? '{}' } catch { return memory } }
let cache: { raw: string; ctx: WorldContext } = { raw: '{}', ctx: {} }
function current(): WorldContext {
  const r = raw()
  if (r === cache.raw) return cache.ctx
  let ctx: WorldContext = {}
  try { const v = JSON.parse(r); if (v && typeof v === 'object') ctx = v as WorldContext } catch { /* unreadable: none */ }
  cache = { raw: r, ctx }
  return ctx
}
function save(ctx: WorldContext) {
  memory = JSON.stringify(ctx)
  try { sessionStorage.setItem(KEY, memory) } catch { /* memory holds it */ }
  for (const l of listeners) l()
}

/**
 * Note the context of an address: what its place's adapter reads from it. A place without an adapter, or an address
 * that names nothing, leaves the last context as it was, so passing through a home page does not forget the focus.
 * Time is not noted here: only Signals' pin writes it (箭10 2026-11-25).
 */
export function noteAddress(hash: string): void {
  const adapter = adapters.get(placeOf(hash))
  if (!adapter) return
  const read = adapter.toContext(hash)
  if (read.scope == null && read.focus == null) return
  const before = current()
  const next: WorldContext = { ...before, ...(read.scope != null ? { scope: read.scope } : {}), ...(read.focus != null ? { focus: read.focus } : {}) }
  if (JSON.stringify(next) !== JSON.stringify(before)) save(next)
}

/** For tests: forget the context. */
export function resetSharedContext(): void {
  save({})
}

let installed = false
function install() {
  if (installed || typeof window === 'undefined') return
  installed = true
  noteAddress(window.location.hash)
  window.addEventListener('hashchange', () => noteAddress(window.location.hash))
}
const subscribe = (l: () => void) => { install(); listeners.add(l); return () => { listeners.delete(l) } }

export const useWorldContext = (): WorldContext => useSyncExternalStore(subscribe, current, () => ({}))

// --- the frames' place links ---------------------------------------------------------------------------------------

function withFrom(href: string, focus: Focus): string {
  const [route, query = ''] = href.split('?')
  const q = new URLSearchParams(query)
  q.set('from', focusText(focus))
  return `${route}?${q.toString()}`
}

/**
 * The frame's place links with the shared context, for a frame at `here`. A link the host gives as the place's bare
 * home is always filled from the context (switching never lands on a home page when there is somewhere to go). A link
 * the host already points somewhere is kept, unless the place we are on has an adapter (so the context is this
 * place's, not a stale one) and the target shows its focus: then the focus wins. A focus the target does not show
 * travels as from=.
 */
export function placeHrefs<T extends Partial<Record<Place, string>>>(hrefs: T, ctx: WorldContext, here?: Place): T {
  const out: Partial<Record<Place, string>> = { ...hrefs }
  if (ctx.scope == null && ctx.focus == null) return hrefs
  const fresh = here != null && adapters.has(here)
  for (const place of Object.keys(PLACE_HOME) as Place[]) {
    if (place === here) continue
    const adapter = adapters.get(place)
    if (!adapter) continue
    const to = adapter.hrefFor(ctx)
    if (!to) continue
    const given = hrefs[place]
    const bare = given == null || given === PLACE_HOME[place]
    if (!bare && !(fresh && to.shown && ctx.focus)) continue
    out[place] = !to.shown && ctx.focus ? withFrom(to.href, ctx.focus) : to.href
  }
  return out as T
}

/** The hook the frames use. */
export function usePlaceHrefs<T extends Partial<Record<Place, string>>>(hrefs: T, here: Place): T {
  return placeHrefs(hrefs, useWorldContext(), here)
}

/** What an address says it came with but does not show (from=), for the frame's second row. */
export function cameWith(hash: string): Focus | undefined {
  const query = hash.split('?')[1] ?? ''
  return focusOf(new URLSearchParams(query).get('from'))
}
