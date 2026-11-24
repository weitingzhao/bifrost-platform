import type { DeliveryLanesData } from '@bifrost/ui/city/lanes'
import { engineUnreachableText, readEngine } from '@/api/engine'
import { formatShellHash } from '@/lib/shell/consoleRoutes'

/*
 * Work → Delivery (箭10 2026-11-24, the delivery lane, step 3; the Owner: release CI and the Gitea lineage on one page,
 * one Change-Id one lane, the stuck ones first). The lanes come only from the UI engine
 * (GET /engine/v1/delivery/lanes, field table cockpit/ui-engine/delivery-lanes-fields.md); the engine judges every
 * cell, the page draws. Where the engine is not switched on, the page says so: there is no direct read to fall back to.
 */

export const ENGINE_LANES_PATH = '/engine/v1/delivery/lanes'
/** Where the lanes are read, by the platform's viewer_env. PROD has no engine yet. */
export const ENGINE_LANES: Readonly<Partial<Record<string, boolean>>> = { stg: true }
export const engineLanesOn = (env: string | null | undefined): boolean => {
  const key = (env ?? '').trim().toLowerCase()
  return key !== '' && Object.hasOwn(ENGINE_LANES, key) && ENGINE_LANES[key] === true
}
export const LANES_LIMIT = 30

export async function fetchLanes(signal?: AbortSignal): Promise<DeliveryLanesData & { capturedAt: string }> {
  const a = await readEngine<DeliveryLanesData>(ENGINE_LANES_PATH, { query: { limit: String(LANES_LIMIT) }, signal })
  if (!a.reached) throw new Error(engineUnreachableText(a))
  if (a.data == null || !Array.isArray(a.data.lanes)) {
    throw new Error('delivery lanes (engine): ' + (a.evidence.map(e => e.detail ?? e.outcome).join('; ') || 'no data'))
  }
  return { lanes: a.data.lanes, sources: a.data.sources ?? [], oldestRun: a.data.oldestRun ?? null, capturedAt: a.capturedAt }
}

/** The page's address; `line` picks one lane (its Change-Id), so City and a commit can point straight at it (World F1). */
export const deliveryHref = (line?: string) =>
  formatShellHash('work', new URLSearchParams({ section: 'delivery', ...(line ? { line } : {}) }).toString())

/** Back to City's Delivery district (World F2): the lanes are the delivery domain's. */
export const OPEN_IN_CITY = formatShellHash('city', 'at=delivery')

/** "3 lanes · 1 stuck"; an unread list is "?" (World A5), never 0. */
export function lanesLine(data: Pick<DeliveryLanesData, 'lanes'> | null): string {
  if (!data) return '? lanes · ? stuck'
  const stuck = data.lanes.filter(l => l.stuck).length
  return `${data.lanes.length} ${data.lanes.length === 1 ? 'lane' : 'lanes'} · ${stuck} stuck`
}
