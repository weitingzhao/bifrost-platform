import { fromTiled, toTiled, type Doc, type TiledMap } from './builderDoc'

/**
 * Where the Owner's layout lives for now: this browser, with a file he can
 * export and import to carry it elsewhere (the coordinator, 2026-11-18: the
 * platform's store comes later, through a contract, behind the same two calls).
 */

export type LayoutStore = { load: () => Promise<TiledMap | null>; save: (m: TiledMap) => Promise<void> }
type KV = Pick<Storage, 'getItem' | 'setItem'>

export const LAYOUT_KEY = 'bifrost.city.layout.v1'
export const LAYOUT_FILE = 'bifrost-city-layout.json'

export function browserLayoutStore(key = LAYOUT_KEY, storage: () => KV = () => window.localStorage): LayoutStore {
  return {
    // Unreadable or damaged storage opens as bare land rather than breaking the page.
    load: async () => {
      try { const s = storage().getItem(key); return s ? JSON.parse(s) as TiledMap : null } catch { return null }
    },
    save: async m => { storage().setItem(key, JSON.stringify(m)) },
  }
}

export const layoutFileText = (d: Doc) => `${JSON.stringify(toTiled(d), null, 2)}\n`

/** A layout from a file's text; null when it is not one. */
export function parseLayoutFile(text: string): Doc | null {
  try { return fromTiled(JSON.parse(text)) } catch { return null }
}
