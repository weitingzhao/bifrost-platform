import { authedFetch } from '@/api/client'
import type { TiledMap } from './builderDoc'
import { browserLayoutStore, type LayoutStore } from './layoutStore'

/**
 * The layout on the platform (GET/PUT /api/v1/home/layout, contract in the
 * platform's package homelayout), with this browser's copy behind it: the
 * copy opens the page when the platform has none or cannot be reached, and
 * holds the work when a save does not go through.
 */

export type LayoutRequest = (method: 'GET' | 'PUT', body?: unknown) => Promise<{ status: number; json: () => Promise<unknown> }>
type Remote = { version: number; layout: TiledMap | null }

export const SAVED_ELSEWHERE = 'Saved on another device. Export this layout, then reload.'

const withStatus = async (_prefix: string, r: Response) => Object.assign(new Error(`HTTP ${r.status}`), { status: r.status })

export const platformRequest: LayoutRequest = async (method, body) => {
  try {
    const r = await authedFetch('home layout', '/api/v1/home/layout', { method, body: body === undefined ? undefined : JSON.stringify(body) }, withStatus)
    return { status: r.status, json: () => r.json() }
  } catch (e) {
    // No answer at all is status 0: the save did not reach the platform.
    return { status: (e as { status?: number }).status ?? 0, json: async () => ({}) }
  }
}

export function platformLayoutStore(request: LayoutRequest = platformRequest, local: LayoutStore = browserLayoutStore()): LayoutStore {
  let version = 0
  return {
    load: async () => {
      const r = await request('GET')
      if (r.status === 200) {
        const s = await r.json() as Remote
        version = s.version
        if (s.version > 0 && s.layout) return s.layout
      }
      return local.load()
    },
    save: async m => {
      await local.save(m)
      const r = await request('PUT', { base_version: version, layout: m })
      if (r.status === 200) { version = (await r.json() as Remote).version; return }
      if (r.status === 409) throw new Error(SAVED_ELSEWHERE)
      // Without the table or the database the browser's copy is the save.
      if (r.status !== 503) throw new Error('Saved in this browser only: the platform refused it.')
    },
  }
}
