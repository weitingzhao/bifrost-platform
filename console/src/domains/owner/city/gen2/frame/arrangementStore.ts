import type { Arrangement } from './grid'

/**
 * Where the Owner's positions are kept for now: this browser (one record per
 * place, by its address). Display only: losing it loses nothing but where
 * things stand, and the default packing takes over. Moving it to the platform
 * (so his phone and his computer agree) is a later step behind the same calls.
 */

export type ArrangementStore = { get: (place: string) => Arrangement; set: (place: string, a: Arrangement | null) => void }
type KV = Pick<Storage, 'getItem' | 'setItem'>

// v2: a record per place is { plan, at } (how its ground is cut into plots, which plot each thing takes).
export const ARRANGEMENT_KEY = 'bifrost.city.gen2.arrangement.v2'

const valid = (a: unknown): a is Arrangement => !!a && typeof a === 'object' && (!('plan' in a) || typeof (a as Arrangement).plan === 'string')

export function browserArrangements(key = ARRANGEMENT_KEY, storage: () => KV = () => window.localStorage): ArrangementStore {
  const read = (): Record<string, Arrangement> => {
    try { const s = storage().getItem(key); return s ? JSON.parse(s) as Record<string, Arrangement> : {} } catch { return {} }
  }
  return {
    get: place => { const a = read()[place]; return valid(a) ? a : {} },
    set: (place, a) => {
      const all = read()
      if (a) all[place] = a
      else delete all[place]
      // A full or blocked storage only means the positions are not kept past this page.
      try { storage().setItem(key, JSON.stringify(all)) } catch { /* not kept */ }
    },
  }
}
