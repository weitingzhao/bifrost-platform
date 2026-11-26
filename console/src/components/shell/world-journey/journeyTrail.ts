import { useSyncExternalStore } from 'react'
import { visit, worldLocation } from './location'

/**
 * The journey's trail, one for every view (箭10 2026-11-23: the common journey bar is the engine's WorldFrame;
 * its return slot is fed from here, the World line's). Each place change appends the place left; going back to
 * the last stop (the frame's Return, or the browser's Back) unwinds it instead of looping (location.ts visit).
 * Kept in sessionStorage, so a reload keeps where Return goes (an address, not a snapshot: the place opens with
 * its current data). A view opened directly has no previous stop and shows no Return.
 */
const KEY = 'bifrost.world.trail'
const listeners = new Set<() => void>()
// Storage off (private window, blocked): the trail lives in memory for this page only.
let memory = '[]'
const raw = (): string => { try { return sessionStorage.getItem(KEY) ?? '[]' } catch { return memory } }
let cache: { raw: string; stops: string[] } = { raw: '[]', stops: [] }
/** The trail; the same array while the stored text is the same (a stable snapshot for React). */
function current(): string[] {
  const r = raw()
  if (r === cache.raw) return cache.stops
  let stops: string[] = []
  try { const v = JSON.parse(r); if (Array.isArray(v)) stops = v.filter((x): x is string => typeof x === 'string') } catch { /* unreadable: start over */ }
  cache = { raw: r, stops }
  return stops
}

export function step(from: string, to: string) {
  const before = current()
  const next = visit(before, from, to)
  if (next === before) return
  memory = JSON.stringify(next)
  try { sessionStorage.setItem(KEY, memory) } catch { /* memory holds it */ }
  for (const l of listeners) l()
}

let installed = false
function install() {
  if (installed || typeof window === 'undefined') return
  installed = true
  window.addEventListener('hashchange', e => step(new URL(e.oldURL).hash, new URL(e.newURL).hash))
}

const subscribe = (l: () => void) => { install(); listeners.add(l); return () => { listeners.delete(l) } }

/** Every stop, oldest first (the Places panel reads it). */
export const useJourneyTrail = (): readonly string[] => useSyncExternalStore(subscribe, current, () => [])

/** A Console address's page name: the section if it has one, else the route ("status" → "Status"). */
function pageOf(hash: string): string {
  const [route, query = ''] = hash.replace(/^#/, '').split('?')
  const name = new URLSearchParams(query).get('section') || route
  return name ? name.replace(/-/g, ' ').replace(/^./, c => c.toUpperCase()) : ''
}

/** How the Return link names a stop: the place, and for the Console the page ("Console · Status"). */
export function stopLabel(hash: string): string {
  const place = worldLocation(hash).place
  if (place !== 'Console') return place
  const page = pageOf(hash)
  return page ? `Console · ${page}` : 'Console'
}

/**
 * The frame's return slot: the previous stop of this journey, or nothing when the view was opened directly. A stop
 * that is where you are now is no way back (2026-11-25, PROD at 390: a page loaded afresh at #observation kept a
 * trail ending in #observation and offered "Return to Signals" on Signals itself).
 */
export function backOf(stops: readonly string[], here = ''): { label: string; href: string } | undefined {
  const last = [...stops].reverse().find(s => s !== here)
  return last ? { label: stopLabel(last), href: last } : undefined
}

export const useJourneyBack = () => backOf(useJourneyTrail(), typeof window === 'undefined' ? '' : window.location.hash)
